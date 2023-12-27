package avl

import "math"

const (
	l = 0 // left
	r = 1 // right
)

func flip(d int) int {
	return 1 - d
}

// node is a generic type that represents a node in the AVL Tree.
type node[T any] struct {
	// c[0] is less than i, c[1] is greater than i.
	// Using a fixed array instead of distinct fields lets reduce the number of methods on node.
	c [2]*node[T]
	// Generation and height of the node.
	// Height is the least significant 8 bits of this field, and starts at 1.  This limits the total
	// tree height to 254.  Since the worst-case height of an AVL tree is 1.44(log(n)), this is not an issue
	// we need to worry about with the amount of memory a flat-mapped 64 (or 128) bit processor can access.
	// Generation is the rest of the bits, which will overflow after 1<<56 insert or delete operations.
	// If you issue one of those per nanosecond you will overflow in a year or so.  In the unlikely
	// event you encounter this scenario, that insert or delete operation will make a new copy of the
	// whole tree instead of only copying what is needed for that particular operation.
	genH uint64
	i    T // The item the node is holding.
}

const (
	// Trees can have a max height of 255. Good luck hitting this in practice.
	hMask = uint64(0xff)
	// Height only takes the last 8 bits in genH.
	hOffset = 8
	// The largest gen a tree can have. If incrementing gen on a tree would overflow this value,
	// we will copy all the nodes in the tree and reset gen to 0. Modifying a tree once every
	// nanosecond for around a year will overflow maxGen, so in practice it should not happen too often.
	maxGen = uint64(math.MaxUint64) >> hOffset
)

// gen returns this node's generation.
func (n *node[T]) gen() uint64 {
	return n.genH >> hOffset
}

// h returns the node's height in the tree from the least significant byte of genH.
// This limits the tree height to 255, but given that the wost case height of
// an AVL tree is 1.44(log(n)) we will never overflow it on a 64 bit system
func (n *node[T]) h() uint64 {
	return n.genH & hMask
}

func (n *node[T]) balance() (res int) {
	if n.c[l] != nil {
		res -= int(n.c[l].h())
	}
	if n.c[r] != nil {
		res += int(n.c[r].h())
	}
	return
}

// hAndBalance calculates the relative balance of a node.
// Negative numbers indicate a subtree that is left-heavy,
// and positive numbers indicate a Tree that is right-heavy.
func (n *node[T]) hAndBalance() (maxChildHeight uint64, res int) {
	if n.c[l] != nil {
		maxChildHeight = n.c[l].h()
	}
	res -= int(maxChildHeight)
	if n.c[r] != nil {
		h := n.c[r].h()
		res += int(h)
		if h > maxChildHeight {
			maxChildHeight = h
		}
	}
	return
}

// maxChildHeight returns the height of the tallest child node.
// If there is no child node, return 0
func (n *node[T]) maxChildHeight() uint64 {
	h := uint64(0)
	for i := range n.c {
		if n.c[i] == nil {
			continue
		}
		if h < n.c[i].h() {
			h = n.c[i].h()
		}
	}
	return h
}

func (n *node[T]) setH(h uint64) {
	n.genH &= ^hMask
	n.genH |= h
}

// setHeight calculates the height of this node.
func (n *node[T]) setHeight() {
	n.setH(n.maxChildHeight() + 1)
}

/*
|	rotate flips the subtree at n between the following forms:
|	 |           |
|	 n    <=>    m
|	/ \         / \
|  x   m       n   z
|	  / \     / \
|    y   z   x   y
*/
func (n *node[T]) rotate(from, to int) (m *node[T]) {
	m = n.c[to]
	n.c[to] = m.c[from]
	m.c[from] = n
	return
}

// getExact fills nodeStack with the path thru Tree to v, returning the direction
// that a node should be added or removed from.
func (t *Tree[T]) getExact(ins *nodeStack[T], v T) (res int) {
	ins.clear()
	ins.add(t.root)
	var dir int
	for n := t.root; n != nil; {
		if t.less(n.i, v) {
			dir, res = r, Greater
		} else if t.less(v, n.i) {
			dir, res = l, Less
		} else {
			res = Equal
			break
		}
		if n.c[dir] == nil {
			break
		}
		ins.addDir(n.c[dir], dir)
		n = n.c[dir]
	}
	return
}

// getNext returns the next node to consider when trying to delete
// a non-leaf node.
func (n *node[T]) getNext(res *nodeStack[T]) {
	var dir, od int
	for dir = range n.c {
		if n.c[dir] != nil {
			od = flip(dir)
			break
		}
	}
	res.addDir(n.c[dir], dir)
	n = n.c[dir]
	for n.c[od] != nil {
		res.addDir(n.c[od], od)
		n = n.c[od]
	}
}

func (n *node[T]) nextAt(dir int) *node[T] {
	for n.c[dir] != nil {
		n = n.c[dir]
	}
	return n
}

// swapChild swizzles a child pointer from was to is.
func (n *node[T]) swapChild(was, is *node[T]) *node[T] {
	for i := range n.c {
		if n.c[i] == was {
			n.c[i] = is
			return is
		}
	}
	panic("Impossible")
}
