package incr

// TransientProjectionRejected marks a private node with at least one
// unauthenticated compact projection. The publisher exclusively owns the
// arena and clears this flag on every exit before exposing the tree. This
// occupies the remaining high bit in the facade's uint16 node flags without
// changing the node layout.
const TransientProjectionRejected uint16 = 1 << 15
