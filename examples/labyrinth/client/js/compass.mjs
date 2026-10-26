// Screen-space bearing of a world direction relative to the camera.
//
// The camera's screen-right vector is camera.right() = [-cos yaw, 0,
// sin yaw], while a positive yaw step turns the view toward +X (screen
// left). CSS rotate() is clockwise on screen. Therefore an enemy on the
// player's right has to point at +PI/2:
//
//   bearing = yaw - atan2(dx, dz)
//
// The old code used atan2(...) - yaw, which mirrored every threat arrow.
// Kept pure and free of DOM so the sign is pinned by a node test.

export function relativeBearing(yaw, dx, dz) {
    return yaw - Math.atan2(dx, dz);
}
