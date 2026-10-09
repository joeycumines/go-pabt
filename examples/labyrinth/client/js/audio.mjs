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
        this.noiseBuf = null;
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

    _distGain(dist) {
        return Math.max(0.05, Math.min(1, 1 - dist / 18));
    }

    _noiseBuffer() {
        if (this.noiseBuf) return this.noiseBuf;
        const len = Math.floor(this.ctx.sampleRate * 0.5);
        const buf = this.ctx.createBuffer(1, len, this.ctx.sampleRate);
        const d = buf.getChannelData(0);
        for (let i = 0; i < len; i++) d[i] = Math.random() * 2 - 1;
        this.noiseBuf = buf;
        return buf;
    }

    // Crossfire impact: filtered noise crack over a low metallic thud.
    playClash(dist) {
        if (!this.ctx) return;
        const g = this._distGain(dist);
        const src = this.ctx.createBufferSource();
        src.buffer = this._noiseBuffer();
        const f = this.ctx.createBiquadFilter();
        f.type = 'bandpass';
        f.frequency.value = 900;
        const gain = this.ctx.createGain();
        gain.gain.setValueAtTime(0.35 * g, this.ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.001, this.ctx.currentTime + 0.25);
        src.connect(f); f.connect(gain); gain.connect(this.ctx.destination);
        src.start();
        src.stop(this.ctx.currentTime + 0.25);
        const osc = this.ctx.createOscillator();
        osc.type = 'triangle';
        osc.frequency.setValueAtTime(220, this.ctx.currentTime);
        osc.frequency.exponentialRampToValueAtTime(60, this.ctx.currentTime + 0.2);
        const og = this.ctx.createGain();
        og.gain.setValueAtTime(0.3 * g, this.ctx.currentTime);
        og.gain.exponentialRampToValueAtTime(0.001, this.ctx.currentTime + 0.2);
        osc.connect(og); og.connect(this.ctx.destination);
        osc.start();
        osc.stop(this.ctx.currentTime + 0.2);
    }

    // Creature vocalization: low filtered sawtooth swell.
    playRoar(dist) {
        if (!this.ctx) return;
        const g = this._distGain(dist) * 0.5;
        const osc = this.ctx.createOscillator();
        osc.type = 'sawtooth';
        osc.frequency.setValueAtTime(110, this.ctx.currentTime);
        osc.frequency.linearRampToValueAtTime(70, this.ctx.currentTime + 0.6);
        const f = this.ctx.createBiquadFilter();
        f.type = 'lowpass';
        f.frequency.value = 400;
        const gain = this.ctx.createGain();
        gain.gain.setValueAtTime(0.0001, this.ctx.currentTime);
        gain.gain.linearRampToValueAtTime(0.25 * g, this.ctx.currentTime + 0.1);
        gain.gain.exponentialRampToValueAtTime(0.001, this.ctx.currentTime + 0.7);
        osc.connect(f); f.connect(gain); gain.connect(this.ctx.destination);
        osc.start();
        osc.stop(this.ctx.currentTime + 0.7);
    }

    // Struck creature: short harsh blip.
    playHitReaction(dist) {
        if (!this.ctx) return;
        const g = this._distGain(dist);
        const osc = this.ctx.createOscillator();
        osc.type = 'square';
        osc.frequency.setValueAtTime(320, this.ctx.currentTime);
        osc.frequency.exponentialRampToValueAtTime(110, this.ctx.currentTime + 0.18);
        const gain = this.ctx.createGain();
        gain.gain.setValueAtTime(0.22 * g, this.ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.001, this.ctx.currentTime + 0.18);
        osc.connect(gain); gain.connect(this.ctx.destination);
        osc.start();
        osc.stop(this.ctx.currentTime + 0.18);
    }

    // Death throes: long descending groan.
    playDeath(dist) {
        if (!this.ctx) return;
        const g = this._distGain(dist);
        const osc = this.ctx.createOscillator();
        osc.type = 'sawtooth';
        osc.frequency.setValueAtTime(200, this.ctx.currentTime);
        osc.frequency.exponentialRampToValueAtTime(35, this.ctx.currentTime + 0.8);
        const gain = this.ctx.createGain();
        gain.gain.setValueAtTime(0.3 * g, this.ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.001, this.ctx.currentTime + 0.8);
        osc.connect(gain); gain.connect(this.ctx.destination);
        osc.start();
        osc.stop(this.ctx.currentTime + 0.8);
    }
}
