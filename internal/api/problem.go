package api

// ProblemDetails modela un error como lo pide RFC 9457: un documento
// application/problem+json con un `type` estable, un `status` y un `detail`
// que describe esta ocurrencia sin repetir el documento.
type ProblemDetails struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
}
