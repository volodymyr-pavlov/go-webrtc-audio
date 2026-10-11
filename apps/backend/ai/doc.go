// Package ai is the AI presenter of the radio: a [Host] that, while a track plays, writes what the presenter says
// before the next one with a language model that runs on this machine, speaks it with a speech program, converts the
// clip with ffmpeg and hands it to the shared stream through [stream.Interludes]. The server builds the Host from its
// settings.
//
// The package is flat: one file for each step of the job.
//
//   - host.go runs the steps on their own goroutines, with the time left of the track as the deadline: host.go is the
//     way in, with [HostOptions] and [NewHost].
//   - run.go starts external programs, without a shell.
//   - analyze.go reads the tags, the length and the loudness of a file.
//   - ask.go runs prompts through a language model, with LangChain, and checks what comes back.
//   - tag.go describes a track: its mood, energy, genre and a sentence about its sound.
//   - dj.go writes what the presenter says before a track.
//   - voice.go turns that text into an audio clip.
//
// Everything a model returns is untrusted text. Nothing uses it before it has passed a check written for that use, and
// an answer that keeps failing is replaced by one that needs no model. Track titles go to the model, so the model
// server must be on this machine unless the caller says otherwise.
package ai
