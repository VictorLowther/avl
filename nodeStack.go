package ibtree

// nodeStack keeps track of nodes that are modified during insert and delete operations.
// The node at position 0 is the root of the tree, and the node at position len(n.s)-1 is
// always the current working node of the subset of the tree we are working with.
type nodeStack[T any] struct {
	s   []*node[T] // The stack of nodes we are currently manipulating.
	gen uint64     // The generation of the tree we are operating on.
}

// Clear the nodeStack for reuse in a new operation.
func (ns *nodeStack[T]) clear() {
	ns.s = ns.s[:0]
}

// Add a new node[T] to the nodeStack.  All nodes are added at the leaf, so get height 1
func (ns *nodeStack[T]) newNode(v T) *node[T] {
	return &node[T]{i: v, genH: (ns.gen << hOffset) | 0x01}
}

// copy makes a copy of the passed-in node if it is of a different gen than the tree.
func (ns *nodeStack[T]) copy(n *node[T]) *node[T] {
	if n.gen() == ns.gen {
		return n
	}
	return &node[T]{c: n.c, i: n.i, genH: (ns.gen << hOffset) | (n.h())}
}

// Add the node to the nodeStack.
func (ns *nodeStack[T]) add(n *node[T]) {
	ns.s = append(ns.s, ns.copy(n))
}

// Add a node to the nodeStack at the appropriate direction from the current leaf.
func (ns *nodeStack[T]) addDir(n *node[T], dir int) {
	i := len(ns.s)
	ns.s = append(ns.s, ns.copy(n))
	ns.s[i-1].c[dir] = ns.s[i]
}

// pos implements relative positional addressing in the nodeStack.
// Positive integers refer to offset from the root node in the tree,
// negative integers rever to offset from the leaf node in the subset of
// the tree we are working with.
func (ns *nodeStack[T]) pos(i int) int {
	if i >= 0 {
		return i
	}
	return len(ns.s) + i
}

// Return the node at the relative position we are interested in.
func (ns *nodeStack[T]) at(i int) *node[T] {
	return ns.s[ns.pos(i)]
}

// Set the node at the relative position we are working with to the passed-in
// node.
func (ns *nodeStack[T]) set(at int, v *node[T]) {
	ns.s[ns.pos(at)] = v
}

// Drop the current leaf of the node stack, and from the tree overall.
func (ns *nodeStack[T]) drop() {
	res := ns.at(-2)
	if res.c[l] == ns.at(-1) {
		res.c[l] = nil
	} else {
		res.c[r] = nil
	}
	ns.set(ns.pos(-1), nil)
	ns.s = ns.s[:ns.pos(-1)]
}

// rebalance walks up the Tree starting at node n, rebalancing nodes
// that no longer meet the AVL balance criteria. rebalance will continue until
// it either walks all the way up the Tree, or the node has the
// same height it started with.
func (ns *nodeStack[T]) rebalance() {
	var n *node[T]
	for i := len(ns.s) - 1; i >= 0; i-- {
		n = ns.s[i]
		var from, to, tooHeavyOn int
		childH, balance := n.hAndBalance()
		switch balance {
		case Less, Equal, Greater:
			// The tree is not too far out of the AVL balance criteria. We don't need to do anything beyond
			// exiting early if the tree height does not change.
			if childH+1 == n.h() {
				// If the node height did not change, we are done.
				return
			} else {
				n.setH(childH + 1)
				continue
			}
		case rightHeavy:
			from, to, tooHeavyOn = r, l, Less
		case leftHeavy:
			from, to, tooHeavyOn = l, r, Greater
		default:
			panic("Tree too far out of shape!")
		}
		n.c[from] = ns.copy(n.c[from])
		if n.c[from].balance() == tooHeavyOn {
			// n.c[from] is balanced such that  rotating n will result in a tree that still
			// violates the AVL balance rules.  Rotate n.c[from] to force the tree into being
			// AVL balanced at the end of this rebalance operation.
			n.c[from].c[to] = ns.copy(n.c[from].c[to])
			n.c[from] = n.c[from].rotate(from, to)
			n.c[from].c[from].setHeight()
		}
		if i > 0 {
			n = ns.s[i-1].swapChild(n, n.rotate(to, from))
		} else {
			n = n.rotate(to, from)
		}
		n.c[to].setHeight()
		ns.s[i] = n
		n.setHeight()
		if childH+1 == n.h() {
			// If the node height did not change, we are done.
			return
		}
	}
}
