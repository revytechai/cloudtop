package gpu

import (
	"strconv"
	"strings"

	"github.com/revytechai/cloudtop/internal/model"
)

// Merge combines what several providers saw into one list of devices.
//
// A GPU that two providers both found is one row, not two: the NVIDIA card
// is visible to the kernel, which publishes its identity, and to NVML, which
// publishes its numbers. The first provider that measured a quantity keeps
// it; a later provider fills in only what is still missing. The row names
// every provider that contributed.
func Merge(lists ...[]model.GPUDevice) []model.GPUDevice {
	var out []model.GPUDevice
	index := make(map[string]int)
	sequence := 0
	for _, list := range lists {
		for _, device := range list {
			sequence++
			key := deviceKey(device)
			if !hasIdentity(device) {
				// Two devices with no identity are still two devices. The
				// position keeps them apart, and the row says so rather
				// than pretending it knows which card it is.
				key = key + "#" + strconv.Itoa(sequence)
				device.Source = markUnidentified(device.Source)
			}
			if i, seen := index[key]; seen {
				out[i] = mergeDevice(out[i], device)
				continue
			}
			index[key] = len(out)
			out = append(out, device)
		}
	}
	return out
}

// deviceKey is the identity a device is merged on.
func deviceKey(d model.GPUDevice) string {
	switch {
	case d.ID != "":
		return d.ID
	case d.BusID != "":
		return d.BusID
	case d.UUID != "":
		return d.UUID
	default:
		return d.Vendor + "/" + d.Model
	}
}

// hasIdentity reports whether a device carries an identity at all.
func hasIdentity(d model.GPUDevice) bool {
	return d.ID != "" || d.BusID != "" || d.UUID != ""
}

// markUnidentified says in the source list that the device has no identity.
func markUnidentified(source string) string {
	if strings.Contains(source, "unidentified") {
		return source
	}
	if source == "" {
		return "unidentified"
	}
	return source + ",unidentified"
}

// mergeDevice folds one provider's reading into an existing row.
func mergeDevice(into, from model.GPUDevice) model.GPUDevice {
	into.Util = firstMeasured(into.Util, from.Util)
	into.MemUsed = firstMeasured(into.MemUsed, from.MemUsed)
	into.MemTotal = firstMeasured(into.MemTotal, from.MemTotal)
	into.Temp = firstMeasured(into.Temp, from.Temp)
	into.Power = firstMeasured(into.Power, from.Power)
	if into.Model == "" {
		into.Model = from.Model
	}
	if into.UUID == "" {
		into.UUID = from.UUID
	}
	if into.BusID == "" {
		into.BusID = from.BusID
	}
	into.Source = joinSources(into.Source, from.Source)
	return into
}

// firstMeasured keeps the first measurement and takes the next one only when
// nothing was measured before.
func firstMeasured(into, next model.Value) model.Value {
	if into.Ok {
		return into
	}
	return next
}

// joinSources names every provider that contributed to a row.
func joinSources(into, next string) string {
	switch {
	case into == "":
		return next
	case next == "" || next == into:
		return into
	default:
		return into + "," + next
	}
}
