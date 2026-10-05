#!/usr/bin/env python3

"""Ask Codex for one language review before a turn finishes."""

import json
import sys
from pathlib import Path


def language_policy():
    """Read the policy stored beside this hook."""
    policy = Path(__file__).resolve().with_name("language-policy.md")
    text = policy.read_text(encoding="utf-8").strip()
    if not text:
        raise ValueError("expected a nonempty language policy")
    return text


def main():
    """Return Codex's Stop decision without reading the chat or editing files."""
    try:
        event = json.load(sys.stdin)
        if not isinstance(event, dict) or event.get("hook_event_name") != "Stop":
            raise ValueError("expected a Stop event")
        active = event.get("stop_hook_active", False)
        if not isinstance(active, bool):
            raise ValueError("expected a boolean stop_hook_active")

        # A Stop continuation already gives Codex a chance to complete its review.
        if active:
            print("{}")
            return 0

        reason = (
            "Before finishing, review the text you wrote or changed in this turn, "
            "including documentation, comments, user-facing text, drafted issues "
            "and pull requests, generated content, and your final response. "
            "Apply the language policy below and correct clear violations within "
            "the user's authorized scope. Preserve technical meaning and established "
            "terms. Review only this turn's work; do not rewrite unrelated content, "
            "change program behavior, or publish additional external changes. "
            "After this single review, finish the turn without requesting another "
            "language review.\n\n" + language_policy()
        )
        print(json.dumps({"decision": "block", "reason": reason}, ensure_ascii=False))
        return 0
    except (OSError, ValueError):
        print("Language review hook could not read its Stop event or language policy.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
