import unittest

from tacit_speech import service_name


class WorkspaceTest(unittest.TestCase):
    def test_service_name_is_stable(self) -> None:
        self.assertEqual(service_name(), "tacit-speech")


if __name__ == "__main__":
    unittest.main()
