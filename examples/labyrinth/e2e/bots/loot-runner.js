// loot-runner.js - Bot source for the BotLab harness.
//
// Collect every elemental power-up on the floor, then leave for the gate.
// The power-up changes the bot's element, so the order it takes them in
// decides which matchups it can win afterwards.

module.exports = {
    name: 'loot-runner',

    tick(ctx, api) {
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
        const goal = (best && bestDist <= 7.0) ? best : api.gate;
        api.moveTo(goal.x, goal.z, { arrive: 0.4, sprint: true });

        const near = api.nearest({ maxDist: 3.2, visibleOnly: true, excludeStunned: true });
        if (near) {
            api.aimAt(near.x, near.z);
            if (!api.losesTo(near.element) || near.dist < 1.2) {
                api.fire(true);
            }
        }
        api.note(goal === best ? ('collect ' + best.elementName) : 'exit');
        return api.command();
    },
};
