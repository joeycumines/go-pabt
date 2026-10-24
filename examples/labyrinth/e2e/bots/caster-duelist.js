// caster-duelist.js - Bot source for the BotLab harness.
//
// Caster archetypes (type 2) stand off and fire bolts, so the bot closes
// on one only when it has the range advantage, then orbits it at a fixed
// radius instead of walking into its bolts.

module.exports = {
    name: 'caster-duelist',

    tick(ctx, api) {
        let target = null;
        if (ctx.frame < 240) {
            for (const caster of api.ofType(2)) {
                if (caster.dist <= 10.0 && (!target || caster.dist < target.dist)) {
                    target = caster;
                }
            }
        }
        const goal = target && target.dist > 3.5 ? target : api.gate;
        api.moveTo(goal.x, goal.z, { arrive: 0.45, sprint: true });

        const threat = (target && target.los && target.dist <= 5.5)
            ? target
            : api.nearest({ maxDist: 4.5, visibleOnly: true, excludeStunned: true });
        if (threat) {
            api.aimAt(threat.x, threat.z);
            if (!threat.stunned && (!api.losesTo(threat.element) || threat.dist < 1.2)) {
                api.fire(true);
            }
            api.note(threat.type === 2 ? 'standoff' : 'defend');
        } else {
            api.note(target ? 'closing on caster' : 'advance to gate');
        }
        return api.command();
    },
};
