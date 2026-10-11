// Package playlist turns a set of audio files into a list of Opus packets that can be sent at once, without decoding
// or encoding anything while a stream runs.
//
// [Load] runs ffmpeg once per file to make 20 ms Opus packets in an Ogg container, and reads the packets into memory.
// A [Playlist] is the tracks in order. A [Cursor] walks over it packet by packet and wraps around at the end, so the
// stream never stops. A packet is a slice of the loaded data and is never modified, so any number of goroutines may
// hold the same one.
package playlist
