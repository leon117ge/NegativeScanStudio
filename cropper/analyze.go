package cropper

// ProxySide is the long side of detection / preview proxies.
const ProxySide = 1200

// EdgeMargin is how far (in proxy pixels) the crop is kept inside detected edges.
const EdgeMargin = 1.5

// AutoRect computes the automatic crop for an image of size W x H.
func AutoRect(W, H int, angle float64, det *Detection, scale, aspect, inset float64) (Rect, bool) {
	var edges []Edge
	if det != nil && det.OK {
		edges = det.Edges
	}
	hp := Constraints(W, H, angle, edges, EdgeMargin*scale)
	return FitRect(hp, W, H, aspect, inset)
}
