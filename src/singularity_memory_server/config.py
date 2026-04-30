"""Compatibility wrapper for the renamed Singularity Memory config module.

Most of the server imports still use ``singularity_memory_server.config``.
The canonical implementation lives in ``singularity_config`` after the
rebrand; keep this shim until the imports are migrated mechanically.
"""

from .singularity_config import *  # noqa: F401,F403
from .singularity_config import _get_raw_config  # noqa: F401
