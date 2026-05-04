package adapter

import "github.com/Mrs4s/MiraiGo/message"

// ElementType represents the type of a message element.
type ElementType int

//go:generate stringer -type ElementType -linecomment
const (
	ElementTypeText          ElementType = iota // 文本
	ElementTypeImage                            // 图片
	ElementTypeFace                             // 表情
	ElementTypeAt                               // 艾特
	ElementTypeReply                            // 回复
	ElementTypeService                         // 服务
	ElementTypeForward                         // 转发
	ElementTypeFile                            // 文件
	ElementTypeVoice                           // 语音
	ElementTypeVideo                           // 视频
	ElementTypeLightApp                        // 轻应用
	ElementTypeRedBag                          // 红包
	ElementTypeDice                           // 骰子
	ElementTypeFingerGuessing                  // 猜拳
	ElementTypeMarketFace                      // 表情包
	ElementTypeAnimatedSticker                 // 动态表情
)

// IMessageElement is the core message element interface.
// It is implemented by both adapter-native segment types and miraigo/message types
// (via miraigo/message/compat.go).
type IMessageElement interface {
	Type() ElementType
	ToSendingMessage() *SendingMessage
}

// SendingMessage is a message ready for sending.
type SendingMessage struct {
	Elements []IMessageElement
}

// Append appends elements to the sending message.
func (s *SendingMessage) Append(elems ...IMessageElement) {
	s.Elements = append(s.Elements, elems...)
}

// region segment types

// TextSegment represents a text message element.
type TextSegment struct {
	Content string
}

func (s *TextSegment) Type() ElementType { return ElementTypeText }
func (s *TextSegment) ToSendingMessage() *SendingMessage {
	return &SendingMessage{Elements: []IMessageElement{s}}
}

// ImageSegment represents an image message element.
type ImageSegment struct {
	File string // file path, url, or base64://...
	Url  string
}

// FaceSegment represents a face (emoji) message element.
type FaceSegment struct {
	Index int32
	Name  string
}

// AtSegment represents an @ mention message element.
type AtSegment struct {
	Target  int64
	Display string
}

// ReplySegment represents a reply message element.
type ReplySegment struct {
	ReplySeq int32
	Id       string
	Sender   int64
	GroupID  int64
	Time     int32
}

func (s *ReplySegment) Type() ElementType { return ElementTypeReply }
func (s *ReplySegment) ToSendingMessage() *SendingMessage {
	return &SendingMessage{Elements: []IMessageElement{s}}
}

// VoiceSegment represents a voice message element.
type VoiceSegment struct {
	Name string
	Md5  []byte
	Size int32
	Url  string
	Data []byte
}

// VideoSegment represents a video message element.
type VideoSegment struct {
	Name      string
	Uuid     []byte
	Size     int32
	ThumbSize int32
	Md5      []byte
	ThumbMd5 []byte
	Url      string
}

// FileSegment represents a file message element.
type FileSegment struct {
	Name  string
	Size  int64
	Path  string
	Id    string
	Url   string
	Busid int32
}

// ForwardSegment represents a forward message element.
type ForwardSegment struct {
	ResId string
}

// JsonSegment represents a JSON/app message element.
type JsonSegment struct {
	Content string
}

// MarketFaceSegment represents a market face message element.
type MarketFaceSegment struct {
	Id   string
	Name string
}

// DiceSegment represents a dice message element.
type DiceSegment struct {
	Value int32
}

// FingerGuessingSegment represents a finger guessing message element.
type FingerGuessingSegment struct {
	Value int32
}

// endregion

// region message types

// GroupMessage represents a group message event.
type GroupMessage struct {
	ID        int64
	GroupCode int64
	GroupName string
	Sender    *SenderInfo
	Time      int64
	Elements  []IMessageElement
}

// PrivateMessage represents a private message event.
type PrivateMessage struct {
	ID      int64
	UserID  int64
	Self    int64
	Sender  *SenderInfo
	Time    int64
	Elements []IMessageElement
}

// endregion

// region message element wrappers

// MessageElementAdapter wraps a miraigo message element as an adapter IMessageElement.
// It allows []message.IMessageElement to satisfy []adapter.IMessageElement.
type MessageElementAdapter struct {
	Elem message.IMessageElement
}

func (a *MessageElementAdapter) Type() ElementType {
	switch a.Elem.(type) {
	case *message.TextElement:
		return ElementTypeText
	case *message.ImageElement, *message.GroupImageElement, *message.FriendImageElement, *message.GuildImageElement:
		return ElementTypeImage
	case *message.FaceElement:
		return ElementTypeFace
	case *message.AtElement:
		return ElementTypeAt
	case *message.ReplyElement:
		return ElementTypeReply
	case *message.ServiceElement, *message.LightAppElement, *message.MusicShareElement:
		return ElementTypeService
	case *message.ForwardElement:
		return ElementTypeForward
	case *message.FileElement, *message.GroupFileElement, *message.FriendFileElement:
		return ElementTypeFile
	case *message.VoiceElement, *message.GroupVoiceElement, *message.RecordElement:
		return ElementTypeVoice
	case *message.VideoElement, *message.ShortVideoElement:
		return ElementTypeVideo
	case *message.RedBagElement:
		return ElementTypeRedBag
	case *message.DiceElement:
		return ElementTypeDice
	case *message.FingerGuessingElement:
		return ElementTypeFingerGuessing
	case *message.MarketFaceElement:
		return ElementTypeMarketFace
	case *message.AnimatedSticker:
		return ElementTypeAnimatedSticker
	default:
		return ElementTypeText
	}
}

func (a *MessageElementAdapter) ToSendingMessage() *SendingMessage {
	return &SendingMessage{Elements: []IMessageElement{a}}
}

// Unwrap returns the underlying miraigo message element.
func (a *MessageElementAdapter) Unwrap() message.IMessageElement {
	return a.Elem
}

// AdaptElements converts []message.IMessageElement to []adapter.IMessageElement.
func AdaptElements(elems []message.IMessageElement) []IMessageElement {
	result := make([]IMessageElement, len(elems))
	for i, e := range elems {
		result[i] = &MessageElementAdapter{Elem: e}
	}
	return result
}

// ToMessageElements converts []adapter.IMessageElement back to []message.IMessageElement.
// This is used at boundaries where an older interface expects []message.IMessageElement.
func ToMessageElements(elems []IMessageElement) []message.IMessageElement {
	result := make([]message.IMessageElement, len(elems))
	for i, e := range elems {
		if a, ok := e.(*MessageElementAdapter); ok {
			result[i] = a.Elem
		}
	}
	return result
}

// endregion
