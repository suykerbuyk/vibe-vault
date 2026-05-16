// TypeScript fixture for verify symbol-grammar tests.

export const MAX_FRAMES = 128;

export interface FrameLike {
    id: number;
}

export class CanMonitor {
    constructor(public name: string) {}
}

export async function runReplayMode(file: string): Promise<void> {
    return;
}

function loadConfig(path: string): object {
    return { path };
}
