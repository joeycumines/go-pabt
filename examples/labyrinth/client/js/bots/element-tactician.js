// element-tactician.js - Built-in persona bot: exploit the element cycle.
//
// Picks up a power-up when one is close, then hunts the actors whose element
// ours beats. A disadvantaged actor is left alone unless it walks into us.

let pressFrames = 0;

export default {
    name: 'element_tactician',
    tick(ctx, api) {
        if (ctx.frame <= 1) pressFrames = 0;
        const self = api.self;

        let best = null;
        let bestDist = Infinity;
        for (const pu of api.powerups) {
            const d = Math.hypot(pu.x - self.x, pu.z - self.z);
            if (d < bestDist) {
                bestDist = d;
                best = pu;
            }
        }
        if (best && bestDist < 6.0) {
            api.moveTo(best.x, best.z, { arrive: 0.4, sprint: true });
            api.note('seek_powerup');
            return api.command();
        }

        if (pressFrames < 70) {
            const prey = api.nearest({ excludeStunned: true, test: (a) => api.beats(a.element) });
            if (prey) {
                pressFrames++;
                api.aimAt(prey.x, prey.z);
                if (prey.dist > 2.5) {
                    api.moveTo(prey.x, prey.z, { arrive: 1.0, sprint: true });
                } else {
                    api.moveWorld(prey.x - self.x, prey.z - self.z, 0.5);
                    api.sprint(false);
                }
                api.note('press_advantage');
                return api.command();
            }
        }

        api.moveTo(api.gate.x, api.gate.z, { arrive: 0.4, sprint: true });
        api.note('advance');
        return api.command();
    },
};
