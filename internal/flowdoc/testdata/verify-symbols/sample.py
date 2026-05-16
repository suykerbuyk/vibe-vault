"""Python fixture for verify symbol-grammar tests."""

MAX_FRAMES = 128

class CanMonitor:
    def __init__(self, name):
        self.name = name

    def start(self):
        return self.name


def load_config(path):
    return {"path": path}


async def run_replay_mode(file):
    return None
