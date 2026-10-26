// Client-side actor table: what each archetype looks like and how wide its
// body is. Type ids match the server's actor tables (game/session.go):
// 0 = brute, 1 = swarm, 2 = caster, 3 = stalker.
//
// VISUAL_HEIGHT is the world-unit height the model is scaled to, so a swarm
// reads small and a brute reads tall at the same distance.
// BODY_RADIUS is the horizontal body radius per archetype. T4's server body
// separation (game/damage.go) will use the same table so the camera cannot
// end up inside a model; it is exported here now so client and server agree
// on one source of truth.
export const VISUAL_HEIGHT = [1.6, 0.8, 1.2, 1.1];
export const BODY_RADIUS = [0.55, 0.35, 0.4, 0.4];
