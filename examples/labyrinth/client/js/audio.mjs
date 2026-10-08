// WebAudio procedural sound engine: heartbeat, shot, stun, ambient drone.
// No audio assets — all sounds synthesized in real time.

export class AudioEngine {
    constructor() {
        this.ctx = null;
        this.heartbeatOsc = null;
        this.heartbeatGain = null;
        this.droneOsc = null;
        this.droneGain = null;
        this._proximity = 10; // distance to nearest minotaur
    }

    resume() {
        if (this.ctx) return;
        this.ctx = new (window.AudioContext || window.webkitAudioContext)();

        // Heartbeat: low-frequency oscillator modulated by proximity.
        this.heartbeatOsc = this.ctx.createOscillator();
        this.heartbeatOsc.type = 'sine';
        this.heartbeatOsc.frequency.value = 40; // 40Hz base
        this.heartbeatGain = this.ctx.createGain();
        this.heartbeatGain.gain.value = 0;
        this.heartbeatOsc.connect(this.heartbeatGain);
        this.heartbeatGain.connect(this.ctx.destination);
        this.heartbeatOsc.start();

        // Ambient drone: low sustained tone.
        this.droneOsc = this.ctx.createOscillator();
        this.droneOsc.type = 'sawtooth';
        this.droneOsc.frequency.value = 55; // A1
        this.droneGain = this.ctx.createGain();
        this.droneGain.gain.value = 0.03; // very quiet
        this.droneOsc.connect(this.droneGain);
        this.droneGain.connect(this.ctx.destination);
        this.droneOsc.start();

        // Update heartbeat volume based on proximity.
        this._updateHeartbeat();
    }

    setHeartbeatProximity(dist) {
        this._proximity = dist;
    }

    _updateHeartbeat() {
        if (!this.ctx) return;
        // Closer minotaur = louder, faster heartbeat.
        const vol = Math.max(0, 1 - this._proximity / 8);
        const freq = 40 + vol * 40; // 40-80Hz
        this.heartbeatGain.gain.setTargetAtTime(vol * 0.3, this.ctx.currentTime, 0.1);
        this.heartbeatOsc.frequency.setTargetAtTime(freq, this.ctx.currentTime, 0.1);
        requestAnimationFrame(() => this._updateHeartbeat());
    }

    playShot() {
        if (!this.ctx) return;
        // Short noise burst.
        const osc = this.ctx.createOscillator();
        osc.type = 'square';
        osc.frequency.value = 800;
        const gain = this.ctx.createGain();
        gain.gain.setValueAtTime(0.2, this.ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.001, this.ctx.currentTime + 0.1);
        osc.connect(gain);
        gain.connect(this.ctx.destination);
        osc.start();
        osc.stop(this.ctx.currentTime + 0.1);
    }

    playStun() {
        if (!this.ctx) return;
        // Descending tone.
        const osc = this.ctx.createOscillator();
        osc.type = 'sine';
        osc.frequency.setValueAtTime(600, this.ctx.currentTime);
        osc.frequency.exponentialRampToValueAtTime(100, this.ctx.currentTime + 0.3);
        const gain = this.ctx.createGain();
        gain.gain.setValueAtTime(0.3, this.ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.001, this.ctx.currentTime + 0.3);
        osc.connect(gain);
        gain.connect(this.ctx.destination);
        osc.start();
        osc.stop(this.ctx.currentTime + 0.3);
    }
}
