// explorer-runner.js - Built-in persona bot: make for the gate, shoot back.
//
// The simplest useful playtest bot. It path-finds to the gate and fires at
// anything that closes within reach, unless the target holds the element
// that beats ours.

export default {
    name: 'explorer_runner',
    tick(ctx, api) {
        const gate = api.gate;
        api.moveTo(gate.x, gate.z, { arrive: 0.4, sprint: true });

        const near = api.nearest({ maxDist: 3.2, visibleOnly: true, excludeStunned: true });
        if (near) {
            api.aimAt(near.x, near.z);
            if (!api.losesTo(near.element) || near.dist < 1.2) {
                api.fire(true);
            }
            api.note('defensive_shot');
        } else {
            api.note('run_gate');
        }
        return api.command();
    },
};
