// Package model holds the data types shared by the collectors, the GPU
// providers and the renderer.
//
// Everything here is a plain value. A snapshot is published immutable and
// read by the renderer without locking.
package model

import "time"

// Value is one measured quantity together with the source that produced it.
//
// A quantity that could not be measured is Missing: ok is false and the
// renderer shows N/A. It is never rendered as zero, because a zero reads as
// "measured, and the answer is nothing", which is a different claim from
// "not available". A GPU with no separate VRAM reports no memory rather than
// zero bytes of memory.
type Value struct {
	Src  string
	Unit string
	V    float64
	Ok   bool
}

// Measured is a value that was read.
func Measured(src, unit string, v float64) Value {
	return Value{Src: src, Unit: unit, V: v, Ok: true}
}

// Missing is a value that could not be read. The source is still recorded,
// so the reader can tell which provider was asked and did not answer.
func Missing(src, unit string) Value {
	return Value{Src: src, Unit: unit, Ok: false}
}

// GPUDevice describes one GPU.
//
// Fields the driver does not expose are Missing rather than zero. ID is
// stable across reboots (PCI address plus UUID), never an enumeration
// index, which changes when the machine comes up with a different order.
type GPUDevice struct {
	ID       string
	Vendor   string
	Model    string
	BusID    string
	UUID     string
	Source   string
	Util     Value
	MemUsed  Value
	MemTotal Value
	Temp     Value
	Power    Value
}

// CPUStat is the CPU view of one snapshot.
type CPUStat struct {
	Total Value
	Cores []Value
}

// Snapshot is one refresh of the machine's state.
//
// Notes carries the reasons a provider could not be read. A GPU that is
// missing from the list because its provider failed must say so, or the
// absence reads as "there is no GPU here".
type Snapshot struct {
	At    time.Time
	CPU   CPUStat
	GPUs  []GPUDevice
	Notes []string
}
