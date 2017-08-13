package physics

import (
	"math"
	"log"
)

type BBTreeVelocityFunc func(obj interface{}) *Vector

type Node struct {
	obj    interface{}
	bb     *BB
	parent *Node

	Children
	Leaf
}

type Children struct {
	a, b *Node
}

type Leaf struct {
	stamp uint
	pairs *Pair
}

type Pair struct {
	a, b        Thread
	collisionId uint
}

type Thread struct {
	prev, next *Pair
	leaf       *Node
}

func (thread *Thread) Unlink() {
	next := thread.next
	prev := thread.prev

	if next != nil {
		if next.a.leaf == thread.leaf {
			next.a.prev = prev
		} else {
			next.b.prev = prev
		}
	}

	if prev != nil {
		if prev.a.leaf == thread.leaf {
			prev.a.next = next
		} else {
			prev.b.next = next
		}
	} else {
		thread.leaf.pairs = next
	}
}

type BBTree struct {
	spatialIndex *SpatialIndex
	velocityFunc *BBTreeVelocityFunc

	leaves map[uint]*Node
	root   *Node

	pooledNodes *Node
	pooledPairs *Pair

	stamp uint
}

func NewBBTree(bbfunc SpatialIndexBB, staticIndex *SpatialIndex) *SpatialIndex {
	bbtree := &BBTree{
		leaves: map[uint]*Node{},
	}
	bbtree.spatialIndex = NewSpatialIndex(bbtree, bbfunc, staticIndex)
	return bbtree.spatialIndex
}

func (tree *BBTree) Destroy() {
	panic("implement me")
}

func (tree *BBTree) Count() int {
	return len(tree.leaves)
}

func (tree *BBTree) Each(f SpatialIndexIterator, data interface{}) {
	for _, v := range tree.leaves {
		f(v.obj, data)
	}
}

func (tree *BBTree) Contains(obj interface{}, hashId uint) bool {
	_, ok := tree.leaves[hashId]
	return ok
}

func (tree *BBTree) Insert(obj interface{}, hashId uint) {
	log.Println("Inserting new leaf")

	leaf := tree.NewLeaf(obj)
	tree.leaves[hashId] = leaf

	root := tree.root
	tree.root = tree.SubtreeInsert(root, leaf)

	leaf.stamp = tree.GetMasterTree().stamp

	tree.LeafAddPairs(leaf)
	tree.IncrementStamp()
}

func (tree *BBTree) IncrementStamp() {
	dynamicTree := tree.spatialIndex.dynamicIndex.GetTree()
	if dynamicTree != nil {
		dynamicTree.stamp++
	} else {
		tree.stamp++
	}
}

type MarkContext struct {
	tree       *BBTree
	staticRoot *Node
	f          SpatialIndexQuery
	data       interface{}
}

func VoidQueryFunc(obj1, obj2 interface{}, collisionId uint, data interface{}) uint {
	return collisionId
}

func (tree *BBTree) LeafAddPairs(leaf *Node) {
	dynamicIndex := tree.spatialIndex.dynamicIndex
	if dynamicIndex != nil {
		dynamicRoot := dynamicIndex.GetRootIfTree()
		if dynamicRoot != nil {
			dynamicTree := dynamicIndex.GetTree()
			context := &MarkContext{dynamicTree, nil, nil, nil}
			dynamicRoot.MarkLeafQuery(leaf, true, context)
		}
	} else {
		staticRoot := tree.spatialIndex.staticIndex.GetRootIfTree()
		context := &MarkContext{tree, staticRoot, VoidQueryFunc, nil}
		leaf.MarkLeaf(context)
	}
}

func (leaf *Node) MarkLeaf(context *MarkContext) {
	tree := context.tree
	if leaf.stamp == tree.GetMasterTree().stamp {
		staticRoot := context.staticRoot
		if staticRoot != nil {
			staticRoot.MarkLeafQuery(leaf, false, context)
		}

		for node := leaf; node.parent != nil; node = node.parent {
			if node == node.parent.a {
				node.parent.b.MarkLeafQuery(leaf, true, context)
			} else {
				node.parent.a.MarkLeafQuery(leaf, false, context)
			}
		}
	} else {
		pair := leaf.pairs
		for pair != nil {
			if leaf == pair.b.leaf {
				pair.collisionId = context.f(pair.a.leaf.obj, leaf.obj, pair.collisionId, context.data)
				pair = pair.b.next
			} else {
				pair = pair.a.next
			}
		}
	}
}
func (subtree *Node) MarkLeafQuery(leaf *Node, left bool, context *MarkContext) {
	if leaf.bb.Intersects(subtree.bb) {
		if subtree.IsLeaf() {
			if left {
				context.tree.PairInsert(leaf, subtree)
			} else {
				if subtree.stamp < leaf.stamp {
					context.tree.PairInsert(subtree, leaf)
				}
				context.f(leaf.obj, subtree.obj, 0, context.data)
			}
		} else {
			subtree.a.MarkLeafQuery(leaf, left, context)
			subtree.b.MarkLeafQuery(leaf, left, context)
		}
	}
}
func (tree *BBTree) PairInsert(a *Node, b *Node) {
	nextA := a.pairs
	nextB := b.pairs
	pair := tree.PairFromPool()
	temp := Pair{
		Thread{next: nextA, leaf: a},
		Thread{next: nextB, leaf: b},
		0,
	}

	a.pairs = pair
	b.pairs = pair
	*pair = temp

	if nextA != nil {
		if nextA.a.leaf == a {
			nextA.a.prev = pair
		} else {
			nextA.b.prev = pair
		}
	}

	if nextB != nil {
		if nextB.a.leaf == b {
			nextB.a.prev = pair
		} else {
			nextB.b.prev = pair
		}
	}
}

func (subtree *BBTree) PairFromPool() *Pair {
	tree := subtree.GetMasterTree()

	pair := tree.pooledPairs

	if pair != nil {
		tree.pooledPairs = pair.a.next
		return pair
	}

	if tree == nil {
		panic("NPE")
	}

	// Pool is exhausted make more
	for i := 0; i < 32; i++ {
		tree.RecyclePair(&Pair{})
	}

	return tree.pooledPairs
}

func (tree *BBTree) RecyclePair(pair *Pair) {
	master := tree.GetMasterTree()
	pair.a.next = master.pooledPairs
	master.pooledPairs = pair
}

func (tree *BBTree) SubtreeInsert(subtree *Node, leaf *Node) *Node {
	if subtree == nil {
		return leaf
	}
	if subtree.IsLeaf() {
		return tree.NewNode(leaf, subtree)
	}

	cost_a := subtree.b.bb.Area() + subtree.a.bb.MergedArea(leaf.bb)
	cost_b := subtree.a.bb.Area() + subtree.b.bb.MergedArea(leaf.bb)

	if cost_a == cost_b {
		cost_a = subtree.a.bb.Proximity(leaf.bb)
		cost_b = subtree.b.bb.Proximity(leaf.bb)
	}

	if cost_b < cost_a {
		NodeSetB(subtree, tree.SubtreeInsert(subtree.b, leaf))
	} else {
		NodeSetA(subtree, tree.SubtreeInsert(subtree.a, leaf))
	}

	subtree.bb = subtree.bb.Merge(leaf.bb)
	return subtree
}

func (tree *BBTree) SubtreeRemove(subtree *Node, leaf *Node) *Node {
	if leaf == subtree {
		return nil
	}

	parent := leaf.parent
	if parent == subtree {
		other := subtree.Other(leaf)
		other.parent = subtree.parent
		tree.RecycleNode(subtree)
		return other
	}

	tree.ReplaceChild(parent.parent, parent, parent.Other(leaf))
	return subtree
}

func (tree *BBTree) ReplaceChild(parent, child, value *Node) {
	if parent.a == child {
		tree.RecycleNode(parent.a)
		NodeSetA(parent, value)
	} else {
		tree.RecycleNode(parent.b)
		NodeSetB(parent, value)
	}

	for node := parent; node != nil; node = node.parent {
		node.bb = node.a.bb.Merge(node.b.bb)
	}
}

func (node *Node) Other(child *Node) *Node {
	if node.a == child {
		return node.b
	}
	return node.a
}

func (node *Node) IsLeaf() bool {
	return node.obj != nil
}

func (tree *BBTree) Remove(obj interface{}, hashId uint) {
	panic("implement me")
}

func (tree *BBTree) Reindex() {
	panic("implement me")
}

func (tree *BBTree) ReindexObject(obj interface{}, hashId uint) {
	panic("implement me")
}

func (tree *BBTree) ReindexQuery(f SpatialIndexQuery, data interface{}) {
	if tree.root == nil {
		return
	}

	// LeafUpdate() may modify tree->root. Don't cache it.
	for _, node := range tree.leaves {
		tree.LeafUpdate(node)
	}

	staticIndex := tree.spatialIndex.staticIndex
	var staticRoot *Node
	if staticIndex != nil {
		staticRoot = staticIndex.class.(*BBTree).root
	}

	context := &MarkContext{tree, staticRoot, f, data}
	tree.root.MarkSubtree(context)

	if staticIndex != nil && staticRoot == nil {
		tree.spatialIndex.CollideStatic(staticIndex, f, data)
	}

	tree.IncrementStamp()
}

func (subtree *Node) MarkSubtree(context *MarkContext) {
	if subtree.IsLeaf() {
		subtree.MarkLeaf(context)
	} else {
		subtree.a.MarkSubtree(context)
		subtree.b.MarkSubtree(context)
	}
}

func (tree *BBTree) LeafUpdate(leaf *Node) bool {
	root := tree.root
	bb := tree.spatialIndex.bbfunc(leaf.obj)

	if !leaf.bb.Contains(bb) {
		leaf.bb = tree.GetBB(leaf.obj)

		root := tree.SubtreeRemove(root, leaf)
		tree.root = tree.SubtreeInsert(root, leaf)

		tree.PairsClear(leaf)
		leaf.stamp = tree.GetMasterTree().stamp
		return true
	}

	return false
}
func (tree *BBTree) PairsClear(leaf *Node) {
	pair := leaf.pairs
	leaf.pairs = nil

	for pair != nil {
		if pair.a.leaf == leaf {
			next := pair.a.next
			pair.b.Unlink()
			tree.RecyclePair(pair)
			pair = next
		} else {
			next := pair.b.next
			pair.a.Unlink()
			tree.RecyclePair(pair)
			pair = next
		}
	}
}

func (tree *BBTree) Query(obj interface{}, bb *BB, f SpatialIndexQuery, data interface{}) {
	panic("implement me")
}

func (tree *BBTree) SegmentQuery(obj interface{}, a, b *Vector, t_exit float64, f SpatialIndexSegmentQuery, data interface{}) {
	panic("implement me")
}

func (tree *BBTree) GetBB(obj interface{}) *BB {
	bb := tree.spatialIndex.bbfunc(obj)
	if tree.velocityFunc != nil {
		coef := 0.1
		x := (bb.R - bb.L) * coef
		y := (bb.T - bb.B) * coef

		v := (*tree.velocityFunc)(obj).Mult(0.1)
		return &BB{
			bb.L + math.Min(-x, v.X),
			bb.B + math.Min(-y, v.Y),
			bb.R + math.Max(x, v.X),
			bb.T + cpfmax(y, v.Y),
		}
	}

	return bb
}

func (tree *BBTree) NewNode(a, b *Node) *Node {
	node := tree.NodeFromPool()
	node.obj = nil
	node.bb = a.bb.Merge(b.bb)
	node.parent = nil

	NodeSetA(node, a)
	NodeSetB(node, b)
	return node
}

func NodeSetA(node, value *Node) {
	node.a = value
	value.parent = node
}

func NodeSetB(node, value *Node) {
	node.b = value
	value.parent = node
}

func (tree *BBTree) NewLeaf(obj interface{}) *Node {
	node := tree.NodeFromPool()
	node.obj = obj
	node.bb = tree.GetBB(obj)
	node.parent = nil
	node.stamp = 0
	node.pairs = nil

	return node
}

func (tree *BBTree) NodeFromPool() *Node {
	node := tree.pooledNodes

	if node != nil {
		tree.pooledNodes = node.parent
		return node
	}

	// Pool is exhausted make more
	for i := 0; i < 32; i++ {
		tree.RecycleNode(&Node{})
	}

	return tree.pooledNodes
}

func (tree *BBTree) RecycleNode(node *Node) {
	node.parent = tree.pooledNodes
	tree.pooledNodes = node
}

func cpfmax(x, y float64) float64 {
	return math.Max(x, y)
}

func (tree *BBTree) GetMasterTree() *BBTree {
	dynamicTree := tree.spatialIndex.dynamicIndex.GetTree()
	if dynamicTree != nil {
		return dynamicTree
	}
	return tree
}
