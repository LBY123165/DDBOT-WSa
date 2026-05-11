package msgstringer

import (
	"testing"

	"github.com/cnxysoft/DDBOT-WSa/adapter"
)

func TestAdapterMsgToString(t *testing.T) {
	m := []adapter.IMessageElement{
		&adapter.FaceSegment{Index: 1},
		&adapter.TextSegment{Content: "q"},
		&adapter.ImageSegment{},
		&adapter.AtSegment{Target: 0},
		&adapter.FileSegment{},
		&adapter.VideoSegment{},
		&adapter.ForwardSegment{},
		&adapter.JsonSegment{},
		&adapter.VoiceSegment{},
		&adapter.ReplySegment{ReplySeq: 199},
		nil,
	}
	AdapterMsgToString(m)
}
