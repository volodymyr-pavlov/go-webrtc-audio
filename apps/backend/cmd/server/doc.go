// Command server streams a playlist of audio files to many WebRTC listeners.
//
// It reads every audio file in the -audio directory once, turns it into Opus packets, and sends the packets to each
// listener that connects over WebRTC. With -web it also serves the built listener page on -http. See the README for the
// flags.
package main
