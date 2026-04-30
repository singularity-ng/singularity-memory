package modelcatalog

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	sizePattern    = regexp.MustCompile(`(?i)(?:^|[:/_\-.])(\d+(?:\.\d+)?)b(?:$|[:/_\-.]|\s)`)
	gemmaEdgeSize  = regexp.MustCompile(`(?i)(?:^|[:/_\-.])e(\d+(?:\.\d+)?)b(?:$|[:/_\-.]|\s)`)
	versionPattern = regexp.MustCompile(`(?i)(?:^|[:/_\-.])(\d{4}|\d+(?:\.\d+)+)(?:$|[:/_\-.])`)
	separatorRun   = regexp.MustCompile(`[-_:/.]+`)
)

func Normalize(providerID, modelID, name string) ModelIdentity {
	id := strings.TrimSpace(modelID)
	lookup := strings.ToLower(id + " " + name)
	params := parseParamBillions(lookup)
	family := inferFamily(id, name)
	version := inferVersion(id)
	return ModelIdentity{
		CanonicalSlug: canonicalSlug(providerID, family, version, params),
		Family:        family,
		Version:       version,
		SizeClass:     sizeClass(params),
		ParamBillions: params,
	}
}

type ModelIdentity struct {
	CanonicalSlug string
	Family        string
	Version       string
	SizeClass     string
	ParamBillions *float64
}

func parseParamBillions(value string) *float64 {
	for _, re := range []*regexp.Regexp{sizePattern, gemmaEdgeSize} {
		match := re.FindStringSubmatch(value)
		if len(match) < 2 {
			continue
		}
		parsed, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		return &parsed
	}
	return nil
}

func sizeClass(params *float64) string {
	if params == nil {
		return ""
	}
	switch {
	case *params <= 8:
		return "tiny"
	case *params <= 14:
		return "small"
	case *params <= 40:
		return "standard"
	case *params <= 100:
		return "large"
	default:
		return "frontier"
	}
}

func inferFamily(modelID, name string) string {
	base := strings.ToLower(modelID)
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	base = strings.TrimSuffix(base, ":free")
	base = strings.ReplaceAll(base, "latest", "")
	base = sizePattern.ReplaceAllString(base, "-")
	base = gemmaEdgeSize.ReplaceAllString(base, "-")
	base = versionPattern.ReplaceAllString(base, "-")
	base = strings.Trim(separatorRun.ReplaceAllString(base, "-"), "-")
	if base == "" && name != "" {
		base = strings.ToLower(name)
		base = separatorRun.ReplaceAllString(base, "-")
		base = strings.Trim(base, "-")
	}
	if strings.Contains(base, "kimi") {
		return "kimi"
	}
	if strings.Contains(base, "glm") {
		return "glm"
	}
	if strings.Contains(base, "ministral") {
		return "ministral"
	}
	if strings.Contains(base, "mistral-small") {
		return "mistral-small"
	}
	return base
}

func inferVersion(modelID string) string {
	normalized := strings.ToLower(modelID)
	if strings.Contains(normalized, "latest") {
		return "latest"
	}
	match := versionPattern.FindStringSubmatch(normalized)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

func canonicalSlug(providerID, family, version string, params *float64) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(providerID))
	b.WriteString("/")
	if family == "" {
		b.WriteString("unknown")
	} else {
		b.WriteString(family)
	}
	if version != "" {
		b.WriteString("-")
		b.WriteString(version)
	}
	if params != nil {
		b.WriteString("-")
		b.WriteString(strconv.FormatFloat(*params, 'f', -1, 64))
		b.WriteString("b")
	}
	return b.String()
}
