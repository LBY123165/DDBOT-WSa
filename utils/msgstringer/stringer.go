package msgstringer

import (
	"strconv"
	"strings"

	"github.com/Mrs4s/MiraiGo/message"
	"github.com/cnxysoft/DDBOT-WSa/adapter"
	"github.com/davecgh/go-spew/spew"
)

// MsgToString converts message elements to a human-readable string.
func MsgToString(elements []message.IMessageElement) string {
	return msgToString(elements)
}

// AdapterMsgToString converts adapter message elements to a human-readable string.
func AdapterMsgToString(elements []adapter.IMessageElement) string {
	var res strings.Builder
	for i, elem := range elements {
		if elem == nil {
			continue
		}
		logger.Debugf(`Element %d is of type %T\n`, i, elem)
		switch e := elem.(type) {
		case *adapter.TextSegment:
			res.WriteString(e.Content)
		case *adapter.FaceSegment:
			res.WriteString("[")
			res.WriteString(e.Name)
			res.WriteString("]")
		case *adapter.ImageSegment:
			res.WriteString("[图片]")
		case *adapter.AtSegment:
			if e.Target == 0 {
				res.WriteString("[艾特全体]")
			} else {
				res.WriteString("[艾特:" + strconv.FormatInt(e.Target, 10) + "]")
			}
		case *adapter.ReplySegment:
			res.WriteString("[回复:")
			res.WriteString(strconv.FormatInt(int64(e.ReplySeq), 10))
			res.WriteString("]")
		case *adapter.FileSegment:
			res.WriteString("[文件]")
			res.WriteString(e.Name)
		case *adapter.VideoSegment:
			res.WriteString("[视频]")
		case *adapter.VoiceSegment:
			res.WriteString("[语音]")
		case *adapter.MessageElementAdapter:
			res.WriteString(legacyElementToString(e.Elem))
		default:
			logger.WithField("content", spew.Sdump(elem)).Debug("found new adapter element")
		}
	}
	return res.String()
}

func msgToString(elements []message.IMessageElement) string {
	var res strings.Builder
	for i, elem := range elements {
		if elem == nil {
			continue
		}
		logger.Debugf(`Element %d is of type %T\n`, i, elem)
		res.WriteString(legacyElementToString(elem))
	}
	return res.String()
}

func legacyElementToString(elem message.IMessageElement) string {
	var res strings.Builder
	switch e := elem.(type) {
	case *message.TextElement:
		res.WriteString(e.Content)
		logger.Debugf(`Content of TextElement: %s\n`, e.Content)
	case *message.FaceElement:
		res.WriteString("[")
		res.WriteString(e.Name)
		res.WriteString("]")
	case *message.GroupImageElement:
		if e.Flash {
			res.WriteString("[闪照]")
		} else {
			res.WriteString("[图片]")
		}
	case *message.FriendImageElement:
		if e.Flash {
			res.WriteString("[闪照]")
		} else {
			res.WriteString("[图片]")
		}
	case *message.ImageElement:
		res.WriteString("[图片]")
	case *message.AtElement:
		if e.Target == 0 {
			res.WriteString("[艾特全体]")
		} else {
			res.WriteString("[艾特:" + strconv.FormatInt(e.Target, 10) + "]")
		}
	case *message.RedBagElement:
		res.WriteString("[QQ红包:")
		res.WriteString(e.Title)
		res.WriteString("]")
	case *message.ReplyElement:
		res.WriteString("[回复:")
		res.WriteString(strconv.FormatInt(int64(e.ReplySeq), 10))
		res.WriteString("]")
	case *message.GroupFileElement:
		res.WriteString("[文件]")
		res.WriteString(e.Name)
	case *message.FriendFileElement:
		res.WriteString("[文件]")
		res.WriteString(e.Name)
	case *message.FileElement:
		res.WriteString("[文件]")
		res.WriteString(e.Name)
	case *message.ShortVideoElement, *message.VideoElement:
		res.WriteString("[视频]")
	case *message.ForwardElement:
		res.WriteString("[聊天记录]")
	case *message.MusicShareElement:
		res.WriteString("[音乐]")
	case *message.LightAppElement:
		res.WriteString("[小程序]")
		res.WriteString(e.Content)
	case *message.ServiceElement:
		res.WriteString("[服务]")
		res.WriteString(e.Content)
	case *message.VoiceElement, *message.GroupVoiceElement, *message.RecordElement:
		res.WriteString("[语音]")
	case *message.MarketFaceElement:
		res.WriteString(e.Name)
	case *message.DiceElement:
		res.WriteString(e.Name)
		res.WriteString(strconv.FormatInt(int64(e.Value), 10))
	case *message.AnimatedSticker:
		res.WriteString("[")
		res.WriteString(e.Name)
		res.WriteString("]")
	case *message.FingerGuessingElement:
		res.WriteString("[")
		res.WriteString(e.Name)
		res.WriteString("]")
	default:
		logger.WithField("content", spew.Sdump(elem)).Debug("found new element")
	}
	return res.String()
}
