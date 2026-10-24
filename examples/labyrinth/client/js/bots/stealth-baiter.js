// stealth-baiter.js - Built-in persona bot: be seen, then break line of sight.
//
// Three phases: walk into an actor's view until it has line of sight and a
// 4.5u approach, sprint away for 45 frames so the actor's Search action has
// to follow a stale last-known position, then hold still out of sight for 60
// frames so the search either converges or gives up.

let phase = 0;
let wait = 0;

export default {
    name: 'stealth_baiter',
    tick(ctx, api) {
        if (ctx.frame <= 1) {
            phase = 0;
            wait = 0;
        }
        const near = api.nearest({});
        if (!near) {
            api.moveTo(api.gate.x, api.gate.z, { arrive: 0.4, sprint: true });
            api.note('bait_no_target');
            return api.command();
        }

        if (phase === 0) {
            if (near.dist <= 4.5 && near.los) {
                wait++;
                api.aimAt(near.x, near.z);
                if (wait >= 15) {
                    phase = 1;
                    wait = 0;
                }
            } else {
                api.moveTo(near.x, near.z, { arrive: 1.0, sprint: true });
            }
            api.note('bait_approach');
        } else if (phase === 1) {
            wait++;
            api.moveTo(api.gate.x, api.gate.z, { arrive: 0.4, sprint: true });
            if (wait >= 45) {
                phase = 2;
                wait = 0;
            }
            api.note('bait_kite');
        } else {
            wait++;
            if (wait >= 60) {
                phase = 0;
                wait = 0;
            }
            if (near.los || near.dist < 4.0) {
                api.moveTo(api.gate.x, api.gate.z, { arrive: 0.4, sprint: true });
            } else {
                api.aimAt(near.x, near.z);
            }
            api.note('bait_observe');
        }
        return api.command();
    },
};
