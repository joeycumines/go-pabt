// Network client: WebSocket connection, snapshot reception, input sending.

export class NetClient {
    constructor() {
        this.ws = null;
        this.token = null;
        this.latestSnap = null;
        this.maze = null; // authoritative maze from server
        this.onEvent = null; // callback(Event)
        this.onMaze = null; // callback(mazeData)
        this.inputSeq = 0;
    }

    connect() {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = `${proto}//${location.host}/ws`;
        this.ws = new WebSocket(url);

        this.ws.onmessage = (e) => {
            try {
                const env = JSON.parse(e.data);
                switch (env.t) {
                    case 'session':
                        this.token = env.p.token;
                        break;
                    case 'maze':
                        this.maze = env.p;
                        if (this.onMaze) this.onMaze(env.p);
                        break;
                    case 'snap':
                        this.latestSnap = {
                            seq: env.w,
                            ack: env.ack,
                            data: env.p,
                        };
                        break;
                    case 'event':
                        if (this.onEvent) {
                            const evt = env.p;
                            this.onEvent(evt);
                        }
                        break;
                }
            } catch (err) {
                console.error('net: parse error', err);
            }
        };

        this.ws.onerror = (e) => console.error('ws error:', e);
        this.ws.onclose = () => console.log('ws closed');
    }

    sendInput(forward, strafe, yaw, pitch, sprint, jump, fire) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        this.inputSeq++;
        const env = {
            t: 'input',
            seq: this.inputSeq,
            p: {
                f: forward,
                s: strafe,
                yaw: yaw,
                pitch: pitch,
                sprint: sprint,
                j: jump,
                fire: fire,
            },
        };
        this.ws.send(JSON.stringify(env));
    }

    // Authoritative debug teleport for the integrated debugging tooling:
    // the server resolves the position against the maze and applies it on
    // the writer goroutine.
    sendDebugTeleport(x, y) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        this.ws.send(JSON.stringify({ t: 'debug', p: { x, y } }));
    }

    getSnapshot() {
        return this.latestSnap;
    }
}
