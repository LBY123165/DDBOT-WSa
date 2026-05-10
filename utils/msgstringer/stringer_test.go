package msgstringer

import (
	"testing"

	"github.com/Mrs4s/MiraiGo/message"
	"github.com/cnxysoft/DDBOT-WSa/adapter"
)

func TestMsgToString(t *testing.T) {
	var m = []message.IMessageElement{
		message.NewFace(1),
		message.NewText("q"),
		&message.GroupImageElement{},
		&message.GroupImageElement{Flash: true},
		&message.FriendImageElement{},
		&message.FriendImageElement{Flash: true},
		message.AtAll(),
		&message.RedBagElement{},
		&message.GroupFileElement{},
		&message.FriendFileElement{},
		&message.ShortVideoElement{},
		&message.ForwardElement{},
		&message.MusicShareElement{},
		&message.LightAppElement{},
		&message.ServiceElement{},
		&message.VoiceElement{},
		&message.ReplyElement{ReplySeq: 199},
		nil,
	}
	MsgToString(m)
}

func TestAdapterMsgToString(t *testing.T) {
	m := []adapter.IMessageElement{
		&adapter.TextSegment{Content: "q"},
		&adapter.ImageSegment{},
		&adapter.AtSegment{Target: 0},
		&adapter.ReplySegment{ReplySeq: 199},
		&adapter.MessageElementAdapter{Elem: message.NewText("legacy")},
		nil,
	}
	AdapterMsgToString(m)
}
