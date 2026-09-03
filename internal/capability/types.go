package capability

const (
	ChatText                     = "chat.completions.text"
	ChatImageInput               = "chat.completions.image_input"
	ChatAudioInput               = "chat.completions.audio_input"
	ResponsesText                = "responses.text"
	ResponsesImageInput          = "responses.image_input"
	ResponsesAudioInput          = "responses.audio_input"
	ImagesGenerations            = "images.generations"
	ImagesEdits                  = "images.edits"
	ImagesVariations             = "images.variations"
	Embeddings                   = "embeddings"
	AudioTranscriptions          = "audio.transcriptions"
	AudioSpeech                  = "audio.speech"
	AudioTranslations            = "audio.translations"
	VideosGenerations            = "videos.generations"
	Realtime                     = "realtime"
	AnthropicMessagesText        = "anthropic.messages.text"
	AnthropicImageInput          = "anthropic.messages.image_input"
	AnthropicTools               = "anthropic.messages.tools"
	AnthropicStructuredOutput    = "anthropic.messages.structured_output"
	AnthropicThinkingAdaptive    = "anthropic.messages.thinking.adaptive"
	AnthropicThinkingManual      = "anthropic.messages.thinking.manual"
	AnthropicPromptCaching       = "anthropic.messages.prompt_caching"
	GoogleInteractionsText       = "google-genai.interactions.text"
	GoogleInteractionsTools      = "google-genai.interactions.tools"
	GoogleInteractionsStructured = "google-genai.interactions.structured-output"
	GoogleInteractionsImage      = "google-genai.interactions.image-input"
	GoogleInteractionsThinking   = "google-genai.interactions.thinking"
	GoogleInteractionsStateful   = "google-genai.interactions.stateful"
	GoogleGenerateText           = "google-genai.generate-content.text"
	GoogleGenerateTools          = "google-genai.generate-content.tools"
	GoogleGenerateStructured     = "google-genai.generate-content.structured-output"
	GoogleGenerateImage          = "google-genai.generate-content.image-input"
	GoogleGenerateThinking       = "google-genai.generate-content.thinking"
	GoogleCountTokens            = "google-genai.count-tokens"
	GoogleEmbedContent           = "google-genai.embed-content"
)

var Known = map[string]bool{
	ChatText: true, ChatImageInput: true, ChatAudioInput: true,
	ResponsesText: true, ResponsesImageInput: true, ResponsesAudioInput: true,
	ImagesGenerations: true, ImagesEdits: true, ImagesVariations: true,
	Embeddings: true, AudioTranscriptions: true, AudioSpeech: true,
	AudioTranslations: true, VideosGenerations: true, Realtime: true,
	AnthropicMessagesText: true, AnthropicImageInput: true, AnthropicTools: true,
	AnthropicStructuredOutput: true, AnthropicThinkingAdaptive: true,
	AnthropicThinkingManual: true, AnthropicPromptCaching: true,
	GoogleInteractionsText: true, GoogleInteractionsTools: true,
	GoogleInteractionsStructured: true, GoogleInteractionsImage: true,
	GoogleInteractionsThinking: true, GoogleInteractionsStateful: true,
	GoogleGenerateText: true, GoogleGenerateTools: true,
	GoogleGenerateStructured: true, GoogleGenerateImage: true,
	GoogleGenerateThinking: true, GoogleCountTokens: true, GoogleEmbedContent: true,
}

type Evidence struct {
	Capability string `json:"capability"`
	Source     string `json:"source"`
	Rule       string `json:"rule,omitempty"`
	Confidence string `json:"confidence"`
	Reason     string `json:"reason"`
}

type Classification struct {
	Model        string     `json:"model"`
	Capabilities []Evidence `json:"capabilities,omitempty"`
	Unknown      bool       `json:"unknown"`
	Reason       string     `json:"reason,omitempty"`
}

func (c Classification) Has(id string) bool {
	for _, evidence := range c.Capabilities {
		if evidence.Capability == id {
			return true
		}
	}
	return false
}
