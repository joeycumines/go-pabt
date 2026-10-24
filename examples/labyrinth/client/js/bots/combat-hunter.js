// combat-hunter.js - Built-in persona bot: engage the nearest visible actor.
//
// It closes on a target, orbits it once it is inside 4u, and holds fire
// while the target holds the element that beats ours - unless the target is
// already on top of us.

let baitWait = 0;

export default {
    name: 'combat_hunter',
    tick(ctx, api) {
        if (ctx.frame <= 1) baitWait = 0;
        let goal = api.gate;
        if (baitWait < 40) {
            const caster = api.nearest({ maxDist: 12, test: (a) => a.type === 2 });
            if (caster) goal = caster;
        }
        api.moveTo(goal.x, goal.z, { arrive: 0.4, sprint: true });

        const target = api.nearest({ visibleOnly: true, maxDist: 4.5 });
        if (target) {
            api.aimAt(target.x, target.z);
            let fire = !target.stunned && (!api.losesTo(target.element) || target.dist < 1.0);
            if (target.type === 2 && baitWait < 40) {
                baitWait++;
                fire = false;
            }
            if (fire) api.fire(true);
            api.note('hunt_engage');
        } else {
            api.note('hunt_advance');
        }
        return api.command();
    },
};
