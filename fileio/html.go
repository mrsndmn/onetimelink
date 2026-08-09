package fileio

import "embed"

var (
	//go:embed *.tmpl
	HtmlTemplates embed.FS
	//go:embed favicon.ico
	Favicon []byte
	//go:embed custom.css
	CustomCss []byte
	//go:embed app.js
	AppJs []byte
)

const (
	// UserMessageViewDefaultText is what the recipient of a link reads above
	// the secret. It can be replaced by dropping userMessageView.txt next to
	// the binary or into the config directory.
	UserMessageViewDefaultText = `Кто-то поделился с вами секретом — например паролем.
	Сохраните его сейчас: ссылка одноразовая и живёт недолго.`
	UserMessageViewFilename = "userMessageView.txt"
	CssFileName             = "custom.css"
	JsFileName              = "app.js"
)
