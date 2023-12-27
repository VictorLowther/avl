package avl

import "sync"

const (
	leftHeavy  = -2
	Less       = -1
	Equal      = 0
	Greater    = 1
	rightHeavy = 2
)

// CompareAgainst is a comparison function that compares a reference item to
// an item in the Tree.
// An example of how it should work:
//
//	comparer := func(reference i) CompareAgainst {
//	    return func(treeItem i) int {
//	        switch {
//	        case LessThan(treeItem, reference): return Less
//	        case LessThan(reference, treeItem): return Greater
//	        default: return Equal
//	        }
//	    }
//	}
//
// CompareAgainst must return:
//
// * Less if the item in the Tree is less than the reference
//
// * Equal if the item in the Tree is equal to the reference
//
// * Greater if the item in the Tree is greater than the reference
type CompareAgainst[T any] func(T) int

// LessThan compares two values to see if the first is LessThan than
// the second.  The Tree code considers any values where neither is LessThan the other
// to be equal.
type LessThan[T any] func(T, T) bool

// Tree is an immutable AVL Tree.  New Tree instances are created whenever any of the Insert or Delete functions
// are called against a Tree.  New Tree instances will share unaltered nodes with the Tree they were created from.
type Tree[T any] struct {
	nsp   *sync.Pool  // Pool of node stacks used to manage tree mutations.  This may be shared among several Trees.
	root  *node[T]    // Root node of the binary tree.
	less  LessThan[T] // Ordering function used to sort nodes in the Tree.
	gen   uint64      // Generation count of the tree.  Every insert or delete call increments gen.
	count int         // Nodes present in the Tree.
}

// getNs fetches a nodeStack from the pool of spare nodestacks.  We cache them in a pool
// to reduce GC pressure in write-heavy operations.
func (t *Tree[T]) getNs() *nodeStack[T] {
	res := t.nsp.Get().(*nodeStack[T])
	res.gen = t.gen
	return res
}

// putNS returns a nodeStack to the pool of spare nodeStacks.
func (t *Tree[T]) putNs(n *nodeStack[T]) {
	n.s = n.s[:cap(n.s)]
	for i := range n.s {
		n.s[i] = nil
	}
	n.s = n.s[:0]
}

// insertOne inserts a single item into the tree.  All Insert* functions use this to do their work.
func (t *Tree[T]) insertOne(ins *nodeStack[T], item T) {
	if t.root == nil {
		t.root = ins.newNode(item)
		t.count = 1
		return
	}
	direction := t.getExact(ins, item)
	n := ins.at(-1)
	// Default to assuming the tree will not need rebalancing.
	var addDir int
	switch direction {
	case Equal:
		n.i = item
		t.root = ins.at(0)
		return
	case Less:
		addDir = l
	case Greater:
		addDir = r
	}
	t.count++
	n.c[addDir] = ins.newNode(item)
	if n.c[flip(addDir)] == nil {
		ins.rebalance()
	}
	t.root = ins.at(0)
}

// New allocates a new Tree that will keep itself ordered according to the passed in LessThan.
func New[T any](lt LessThan[T], items ...T) *Tree[T] {
	res := &Tree[T]{less: lt, nsp: &sync.Pool{New: func() any { return &nodeStack[T]{} }}}
	if len(items) > 0 {
		ins := res.getNs()
		defer res.putNs(ins)
		for i := range items {
			res.insertOne(ins, items[i])
		}
	}
	return res
}

// Fill is a function that is passed another function that can insert
// a single item into a Tree.  It is used by CreateWith and InsertWith to
// amortize costs associated with copy-on-write when performing bulk insert
// operations.
type Fill[T any] func(func(T))

// CreateWith creates a new Tree that is pre-filled with fill, avoiding the overhead
// of copy-on-write operations.
func CreateWith[T any](lt LessThan[T], fill Fill[T]) *Tree[T] {
	res := New[T](lt)
	ins := res.getNs()
	defer res.putNs(ins)
	thunk := func(i T) {
		res.insertOne(ins, i)
	}
	fill(thunk)
	return res
}

// Bud creates a new Tree with the passed-in items
func (t *Tree[T]) Bud(lt LessThan[T], items ...T) *Tree[T] {
	res := &Tree[T]{less: lt, nsp: t.nsp}
	if len(items) > 0 {
		ins := res.getNs()
		defer res.putNs(ins)
		for i := range items {
			res.insertOne(ins, items[i])
		}
	}
	return res
}

// Less returns the current LessThan function that the Tree is using.
func (t *Tree[T]) Less() LessThan[T] {
	return t.less
}

// Cmp takes a reference T and makes a valid CompareAgainst
// using the Tree's current LessThan comparator.
func (t *Tree[T]) Cmp(reference T) CompareAgainst[T] {
	less := t.less
	return func(treeVal T) int {
		if less(treeVal, reference) {
			return Less
		}
		if less(reference, treeVal) {
			return Greater
		}
		return Equal
	}
}

func copyNodes[T any](n *node[T], reverse bool) *node[T] {
	res := &node[T]{genH: n.h(), i: n.i}
	for i := range n.c {
		if n.c[i] != nil {
			res.c[i] = copyNodes(n.c[i], reverse)
		}
	}
	if reverse {
		res.c[r], res.c[l] = res.c[l], res.c[r]
	}
	return res
}

// Fork makes a new copy of the Tree that has the same ordering function and data.
// It will share nodes with the original Tree.
func (t *Tree[T]) Fork() *Tree[T] {
	res := &Tree[T]{less: t.less, root: t.root, count: t.count, nsp: t.nsp, gen: t.gen + 1}
	if res.gen < maxGen {
		return res
	}
	// If you fork a Tree every nanosecond for a year, you will roll over gen and break the copy-on-write invariants.
	// To preserve correctness in that case, if gen gets to maxGens then make a copy of everything in the tree.
	// In practice, you are not likely to ever hit this without really trying.
	res.gen = 0
	if res.root != nil {
		res.root = copyNodes(res.root, false)
	}
	return res

}

// Reverse returns a reversed copy of Tree.  It will not share any resources with Tree.
func (t *Tree[T]) Reverse() *Tree[T] {
	ll := t.less
	res := &Tree[T]{
		nsp:   t.nsp,
		less:  func(a, b T) bool { return ll(b, a) },
		count: t.count,
	}
	if t.root != nil {
		res.root = copyNodes(t.root, true)
	}
	return res
}

// SortBy returns a new empty Tree with an ordering function that falls back to
// t.less if the passed-in LessThan considers two items to be equal.
// This (and SortedClone) can be used to implement trees that will maintain items in
// arbitrarily complicated sort orders.
func (t *Tree[T]) SortBy(l LessThan[T]) *Tree[T] {
	prevLess := t.less
	return &Tree[T]{
		nsp: t.nsp,
		less: func(a, b T) bool {
			switch {
			case l(a, b):
				return true
			case l(b, a):
				return false
			default:
				return prevLess(a, b)
			}
		},
	}
}

// SortedClone makes a new Tree using SortBy, then inserts all the data from t into it.
func (t *Tree[T]) SortedClone(l LessThan[T]) *Tree[T] {
	res := t.SortBy(l)
	ins := res.getNs()
	defer res.putNs(ins)
	for iter := t.All(); iter.Next(); {
		res.insertOne(ins, iter.Item())
	}
	return res
}

// Len returns the number of nodes in the Tree.
func (t *Tree[T]) Len() int { return t.count }

const unorderable = `Unorderable CompareAgainst passed to Get`

// Get returns either the highest item in the Tree that is equal to CompareAgainst and true,
// or a zero T and false if there is no such value in the Tree.
// The Tree must be sorted at the top level in the order that CompareAgainst expects, or you
// will get nonsense results.  If you want to retrieve all
// the items matching CompareAgainst, use one of the Range, Before, or After instead.
func (t *Tree[T]) Get(cmp CompareAgainst[T]) (item T, found bool) {
	h := t.root
top:
	for h != nil {
		switch cmp(h.i) {
		case Greater:
			h = h.c[l]
		case Less:
			h = h.c[r]
		case Equal:
			item, found = h.i, true
			break top
		default:
			panic(unorderable)
		}
	}
	return
}

// Has returns true if the Tree contains an element equal to CompareAgainst.
func (t *Tree[T]) Has(cmp CompareAgainst[T]) bool {
	_, found := t.Get(cmp)
	return found
}

// Fetch returns the exact match for item, true if it is in the Tree,
// or the zero value for T, false if it is not.
func (t *Tree[T]) Fetch(item T) (v T, found bool) {
	n := t.root
	for n != nil {
		if t.less(item, n.i) {
			n = n.c[l]
		} else if t.less(n.i, item) {
			n = n.c[r]
		} else {
			found = true
			v = n.i
			break
		}
	}
	return
}

// Min returns the smallest item in the Tree and true, or a zero T and false if the Tree is empty.
func (t *Tree[T]) Min() (item T, found bool) {
	if t.root != nil {
		found = true
		item = t.root.nextAt(l).i
	}
	return
}

// Max returns the largest item in the Tree and true, or a zero T and false if the Tree is empty.
func (t *Tree[T]) Max() (item T, found bool) {
	if t.root != nil {
		found = true
		item = t.root.nextAt(r).i
	}
	return
}

// InsertWith returns a new Tree that has the data from t and any data returned by fill.
// t and the new Tree will share nodes where possible.
func (t *Tree[T]) InsertWith(fill Fill[T]) *Tree[T] {
	res := t.Fork()
	ins := res.getNs()
	defer res.putNs(ins)
	thunk := func(v T) {
		res.insertOne(ins, v)
	}
	fill(thunk)
	return res
}

// InsertFrom returns a new Tree with data added from a compatible Iter
// t and the new Tree will share nodes where possible.
func (t *Tree[T]) InsertFrom(src Iter[T]) *Tree[T] {
	res := t.Fork()
	ins := res.getNs()
	defer res.putNs(ins)
	for src.Next() {
		res.insertOne(ins, src.Item())
	}
	return res
}

// Insert returns a new Tree that has the data from t and any passed-in data.
// t and the new Tree will share nodes where possible.
func (t *Tree[T]) Insert(item ...T) *Tree[T] {
	res := t.Fork()
	ins := res.getNs()
	defer res.putNs(ins)
	for i := range item {
		res.insertOne(ins, item[i])
	}
	return res
}

// deleteOne deletes a single item from the tree.  All of the Delete* functions use it.
func (t *Tree[T]) deleteOne(ins *nodeStack[T], item T) (deleted T, found bool) {
	if t.root == nil {
		return
	}
	direction := t.getExact(ins, item)
	if found = direction == Equal; !found {
		return
	}
	at := ins.at(-1)
	deleted = at.i
	var alt *node[T]
	for {
		if at.h() == 1 {
			// We are at a leaf node.
			if len(ins.s) > 1 {
				// The leaf is not the root. Nil out the appropriate fork of the
				// parent node and rebalance the tree to maintain AVL invariants.
				ins.drop()
				ins.rebalance()
				t.root = ins.at(0)
			} else {
				// The leaf node is the root, we are deleting the last node in the tree.
				// Reset the tree gen while we are at it.
				t.root = nil
				t.gen = 0
			}
			t.count--
			return
		}
		at.getNext(ins)
		// The node to be deleted is an interior node.  Swap its value with one closer
		// to the leaves and continue.  We will eventually hit a height 1 node and be able
		// to actually delete something.
		alt = ins.at(-1)
		at.i, alt.i = alt.i, at.i
		at = alt
	}
}

// Delete returns a new Tree with the passed-in item removed, along with the removed
// item and whether an item was removed.  The original tree is left unchanged, and the
// returned tree will share nodes where possible.
func (t *Tree[T]) Delete(item T) (into *Tree[T], deleted T, found bool) {
	into = t.Fork()
	ins := into.getNs()
	defer into.putNs(ins)
	deleted, found = into.deleteOne(ins, item)
	return
}

// Erase is a function signature that can be used to bulk delete items from
// a Tree.  The inner function expects a T to be removed from the Tree, and returns
// the value removed and whether the value was found.
type Erase[T any] func(func(T) (T, bool))

func (t *Tree[T]) DeleteWith(erase Erase[T]) *Tree[T] {
	res := t.Fork()
	ins := res.getNs()
	defer res.putNs(ins)
	thunk := func(v T) (deleted T, found bool) {
		deleted, found = res.deleteOne(ins, v)
		return
	}
	erase(thunk)
	return res
}

// DeleteFrom returns a tree that lacks all the items returned by src.
// The original tree is left unchanged.
func (t *Tree[T]) DeleteFrom(src Iter[T]) *Tree[T] {
	res := t.Fork()
	ins := res.getNs()
	defer res.putNs(ins)
	for src.Next() {
		res.deleteOne(ins, src.Item())
	}
	return res
}

// DeleteItems returns a new Tree that lacks items.  The original tree is left unchanged.
func (t *Tree[T]) DeleteItems(items ...T) (into *Tree[T], deleted int) {
	into = t.Fork()
	ins := into.getNs()
	defer into.putNs(ins)
	var found bool
	for i := range items {
		_, found = into.deleteOne(ins, items[i])
		if found {
			deleted++
		}
	}
	return
}
