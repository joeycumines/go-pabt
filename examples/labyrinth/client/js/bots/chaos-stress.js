// chaos-stress.js - Built-in persona bot: fuzz the controls.
//
// The pathing is sane so the bot still covers ground, but a deterministic
// lateral wobble is blended into every move vector and fire is triggered on
// a fixed cadence. This is the persona that shakes out movement, collision
// and netcode edge cases rather than playing well.

export default {
    name: 'chaos_stress',
    tick(ctx, api) {
        const phase = ctx.frame * 0.15;
        const near = api.nearest({ visibleOnly: true, excludeStunned: true, maxDist: 5.5 });

        if (near) {
            api.aimAt(near.x, near.z);
            if (ctx.frame % 20 === 0) api.fire(true);
        } else {
            api.moveTo(api.gate.x, api.gate.z, { arrive: 0.4, sprint: true });
        }

        // Blend a deterministic wobble into the move vector instead of
        // replacing it, so the bot still makes progress towards the gate.
        const current = api.command().move;
        api.moveWorld(current.x + Math.sin(phase) * 0.45, current.z + Math.cos(phase) * 0.45, 1);
        api.note('chaos_fuzz');
        return api.command();
    },
};
