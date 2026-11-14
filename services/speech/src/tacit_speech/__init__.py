"""Tacit speech inference boundary."""

__all__ = ["service_name"]


def service_name() -> str:
    """Return the stable service identity used by smoke tests."""
    return "tacit-speech"
