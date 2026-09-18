// Package auth implements registration, password verification and token
// lifecycle for CoachPulse.
//
// Two kinds of principal exist and are deliberately kept distinct: trainers,
// who authenticate with an email and password, and training clients, who
// receive a portal token scoped to their own record. The distinction lives in
// the token payload and is carried through as a tenancy.Kind, so a handler
// cannot mistake one for the other.
//
// Access tokens are short-lived HS256 JWTs. Refresh tokens are opaque random
// values stored only as a SHA-256 digest and rotated on every use; presenting
// a token that has already been rotated means two parties hold it, so the
// entire family is revoked rather than just that generation.
package auth
