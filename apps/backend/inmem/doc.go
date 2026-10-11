// Package inmem holds small in-memory data structures that keep their state in atomics instead of behind a lock.
//
// [List] is an append-only list that many goroutines may add to and read at the same moment, without a lock.
//
// It exists for the fakes of the tests, which record what the code under test did from goroutines of its own while the
// test looks at it. A mutex around a slice would do, but the program keeps its state in atomics, and its tests do the
// same.
package inmem
