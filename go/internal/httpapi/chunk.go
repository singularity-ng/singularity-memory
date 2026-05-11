package httpapi

import (
	"strconv"
	"strings"
)

const (
	chunkMaxTokens     = 750 // target max tokens per chunk
	chunkOverlapTokens = 50  // tokens to carry over from previous chunk
	chunkMinTokens     = 50  // don't emit a chunk smaller than this
)

// chunkItem splits a retainItem whose content exceeds chunkMaxTokens into
// multiple smaller items with overlapping context. Items that are already
// small enough are returned as-is in a single-element slice.
//
// Each produced item gets a derived DocumentID: "{original}:chunk{N}" so that
// they are stored as separate chunks under the same logical document.
func chunkItem(item retainItem) []retainItem {
	if estimateTokens(item.Content) <= chunkMaxTokens {
		return []retainItem{item}
	}

	texts := splitMarkdown(item.Content, chunkMaxTokens, chunkOverlapTokens)
	if len(texts) <= 1 {
		return []retainItem{item}
	}

	out := make([]retainItem, 0, len(texts))
	for i, t := range texts {
		if strings.TrimSpace(t) == "" {
			continue
		}
		child := item // copy all fields (tags, metadata, timestamps, etc.)
		child.Content = t
		if item.DocumentID != "" {
			child.DocumentID = item.DocumentID + ":chunk" + strconv.Itoa(i+1)
		}
		out = append(out, child)
	}
	if len(out) == 0 {
		return []retainItem{item}
	}
	return out
}

// expandItems runs chunkItem over every item, expanding large ones in-place.
func expandItems(items []retainItem) []retainItem {
	out := make([]retainItem, 0, len(items))
	for _, item := range items {
		out = append(out, chunkItem(item)...)
	}
	return out
}

// splitMarkdown splits text into token-bounded chunks that respect markdown
// heading boundaries and add overlap between consecutive chunks.
//
// Strategy:
//  1. Split the text into sections at heading lines (# / ## / ### …).
//  2. Accumulate sections into a chunk until maxTokens is reached.
//  3. When flushing a chunk, prepend the last ~overlapTokens words from the
//     previous chunk to give the embedding model continuity.
//  4. If a single section is still bigger than maxTokens, fall back to
//     sentence splitting within that section.
func splitMarkdown(text string, maxTokens, overlapTokens int) []string {
	sections := splitOnHeadings(text)

	var chunks []string
	var current []string
	currentTokens := 0
	var prevTail string // overlap carried from the previous chunk

	flush := func() {
		if len(current) == 0 {
			return
		}
		body := strings.Join(current, "\n\n")
		if prevTail != "" {
			body = prevTail + "\n\n" + body
		}
		// capture overlap tail for next chunk
		prevTail = lastWords(body, overlapTokens)
		chunks = append(chunks, strings.TrimSpace(body))
		current = current[:0]
		currentTokens = 0
	}

	for _, sec := range sections {
		t := estimateTokens(sec)
		if t > maxTokens {
			// Section is itself too large — flush what we have, then sentence-split
			flush()
			subChunks := splitBySentence(sec, maxTokens, overlapTokens, prevTail)
			if len(subChunks) > 0 {
				prevTail = lastWords(subChunks[len(subChunks)-1], overlapTokens)
				chunks = append(chunks, subChunks...)
			}
			continue
		}
		if currentTokens > 0 && currentTokens+t > maxTokens {
			flush()
		}
		current = append(current, sec)
		currentTokens += t
	}
	flush()

	// Drop chunks that are too small to be useful (they're likely overlap-only noise)
	out := chunks[:0]
	for _, c := range chunks {
		if estimateTokens(c) >= chunkMinTokens {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return []string{text}
	}
	return out
}

// splitOnHeadings breaks text at lines that start with one or more '#',
// but never inside a fenced code block (``` or ~~~).
// The heading line is included at the start of its section.
func splitOnHeadings(text string) []string {
	lines := strings.Split(text, "\n")
	var sections []string
	var current []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Toggle fence state on ``` or ~~~ delimiters.
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "#") && len(current) > 0 {
			sections = append(sections, strings.Join(current, "\n"))
			current = current[:0]
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		sections = append(sections, strings.Join(current, "\n"))
	}
	return sections
}

// splitBySentence chunks a large block of text by sentence boundaries.
func splitBySentence(text string, maxTokens, overlapTokens int, prevTail string) []string {
	sentences := splitSentences(text)
	var chunks []string
	var current []string
	currentTokens := 0
	tail := prevTail

	flush := func() {
		if len(current) == 0 {
			return
		}
		body := strings.Join(current, " ")
		if tail != "" {
			body = tail + " " + body
		}
		tail = lastWords(body, overlapTokens)
		chunks = append(chunks, strings.TrimSpace(body))
		current = current[:0]
		currentTokens = 0
	}

	for _, s := range sentences {
		t := estimateTokens(s)
		if currentTokens > 0 && currentTokens+t > maxTokens {
			flush()
		}
		current = append(current, s)
		currentTokens += t
	}
	flush()
	return chunks
}

// lastWords returns approximately the last n tokens (words) from text.
func lastWords(text string, n int) string {
	words := strings.Fields(text)
	if len(words) <= n {
		return text
	}
	return strings.Join(words[len(words)-n:], " ")
}
