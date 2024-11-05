package img

import (
	_ "embed"
)

var (
	//go:embed laughing_man.png
	LaughingMan []byte

	// go:embed laughing_man_video.mpg
	LaughingManVideo []byte

	//go:embed laughing_man_mask.png
	LaughingManMask []byte
)
