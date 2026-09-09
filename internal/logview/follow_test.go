package logview

import "testing"

func snapshot(id string, data []byte) Snapshot {
	return Snapshot{Identity: id, Size: int64(len(data)), ReadAt: func(offset int64) ([]byte, error) { return append([]byte(nil), data[offset:]...), nil }}
}

func TestFollowerCompletesSplitLineAndAnnouncesRotationAndTruncate(t *testing.T) {
	follow := NewFollower()
	full := append(line(t, map[string]any{"target": "one"}), '\n')
	if got, err := follow.Poll(snapshot("inode-1", full[:len(full)-1])); err != nil || len(got) != 0 {
		t.Fatalf("first poll got=%#v err=%v", got, err)
	}
	if got, err := follow.Poll(snapshot("inode-1", full)); err != nil || len(records(got)) != 1 {
		t.Fatalf("completion got=%#v err=%v", got, err)
	}
	rotated := append(line(t, map[string]any{"seq": 2, "target": "two", "message": "this makes the replacement longer"}), '\n')
	got, err := follow.Poll(snapshot("inode-2", rotated))
	if err != nil || len(got) != 2 || got[0].Diagnostic.Code != "source_rotated" || got[1].Record.Target() != "two" {
		t.Fatalf("rotation got=%#v err=%v", got, err)
	}
	truncated := append(line(t, map[string]any{"seq": 3, "target": "three"}), '\n')
	got, err = follow.Poll(snapshot("inode-2", truncated))
	if err != nil || len(got) != 2 || got[0].Diagnostic.Code != "source_truncated" || got[1].Record.Target() != "three" {
		t.Fatalf("truncate got=%#v err=%v", got, err)
	}
}
