// gate-runner.js - Bot source for the BotLab harness.
//
// The bot is written against the Bot SDK only: it observes through the api
// object and acts through the api verbs. It never touches the DOM, the
// network or the game state directly.
//
// Contract: a bot source either sets `module.exports = {name, tick}` or
// declares a top-level `function tick(ctx, api)`. `tick` is called once per
// rendered frame and returns the command to apply.

module.exports = {
    name: 'gate-runner',

    tick(ctx, api) {
        const gate = api.gate;

        // Path to the gate over walkable cells. The SDK steers around
        // corners, so the bot never grinds into a wall.
        api.moveTo(gate.x, gate.z, { arrive: 0.45, sprint: true });

        // Fight back only when something is genuinely close.
        const threat = api.nearest({ maxDist: 3.0, visibleOnly: true, excludeStunned: true });
        if (threat) {
            api.aimAt(threat.x, threat.z);
            // Shooting into an elemental disadvantage wastes the pulse.
            if (api.beats(threat.element) || threat.dist < 1.1) {
                api.fire(true);
            }
            api.note('engage ' + threat.typeName);
        } else {
            api.note('advance to gate');
        }
        return api.command();
    },
};
