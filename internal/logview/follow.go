package logview

import "fmt"

// Snapshot is a checked, regular source view. Identity changes when a file is
// replaced; Size is the byte length visible at this deterministic poll.
type Snapshot struct {
	Identity string
	Size     int64
	ReadAt   func(offset int64) ([]byte, error)
}

// Follower turns source snapshots into decoder input. The filesystem adapter
// belongs to the CLI; this state machine is intentionally clock-free.
type Follower struct {
	decoder  *Decoder
	identity string
	offset   int64
	started  bool
}

func NewFollower() *Follower { return &Follower{decoder: NewDecoder()} }

// Poll reads newly visible bytes. It explicitly announces replacement and
// truncation, resets offsets, and never silently duplicates old records.
func (f *Follower) Poll(source Snapshot) ([]Item, error) {
	if source.Identity == "" || source.Size < 0 || source.ReadAt == nil {
		return nil, fmt.Errorf("invalid log source snapshot")
	}
	var out []Item
	if f.started && source.Identity != f.identity {
		out = append(out, f.decoder.Feed(nil, true)...)
		out = append(out, diagnostic("source_rotated", "source rotated"))
		f.offset, f.decoder = 0, NewDecoder()
	} else if f.started && source.Size < f.offset {
		out = append(out, f.decoder.Feed(nil, true)...)
		out = append(out, diagnostic("source_truncated", "source truncated"))
		f.offset, f.decoder = 0, NewDecoder()
	}
	f.started, f.identity = true, source.Identity
	if source.Size == f.offset {
		return out, nil
	}
	data, err := source.ReadAt(f.offset)
	if err != nil {
		return out, fmt.Errorf("read log source: %w", err)
	}
	if int64(len(data)) > source.Size-f.offset {
		return out, fmt.Errorf("log source returned bytes past snapshot size")
	}
	f.offset += int64(len(data))
	return append(out, f.decoder.Feed(data, false)...), nil
}
