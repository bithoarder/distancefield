package main

import (
	"fmt"
	"image"
	_ "image/png"
	"math"
)

type Point struct {
	OutsideDist float64
	InsideDist  float64
}

type Field struct {
	Width, Height int
	Field         []Point
}

func NewFieldFromImage(image *image.Gray) *Field {
	internalField := newFieldFromImage(image)
	internalField.fill()

	field := &Field{Width: image.Bounds().Dx(), Height: image.Bounds().Dy()}
	field.Field = make([]Point, field.Width*field.Height)

	for y := 0; y < field.Height; y++ {
		for x := 0; x < field.Width; x++ {
			soff := (x + 1) + (y+1)*internalField.Width
			doff := x + y*field.Width
			field.Field[doff].InsideDist = math.Sqrt(float64(internalField.Field[soff].ISqDist))
			field.Field[doff].OutsideDist = math.Sqrt(float64(internalField.Field[soff].OSqDist))
		}
	}

	return field
}

func (field *Field) CreateDebugImage() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, field.Width, field.Height))

	for y := 0; y < field.Height; y++ {
		for x := 0; x < field.Width; x++ {
			p := field.Field[x+y*field.Width]

			imgOff := x*4 + y*img.Stride
			img.Pix[imgOff+0] = 0
			img.Pix[imgOff+1] = 0
			img.Pix[imgOff+2] = 0
			img.Pix[imgOff+3] = 255

			if p.InsideDist == p.OutsideDist {
				//fmt.Println(p.InsideDist)
				img.Pix[imgOff+1] = 255
			} else if p.InsideDist > p.OutsideDist {
				if math.Mod(p.InsideDist, 5) <= 2.5 {
					img.Pix[imgOff+0] = 255
				}

				/*if p.InsideDist <= 1 {
					img.Pix[imgOff + 0] = 255
				}*/
				/*else if p.InsideDist >= 5 {
					   if math.Mod(p.InsideDist, 5) <= 2.5 {
						   img.Pix[imgOff + 0] = 255
					   }
				   }*/
			} else {
				//				if p.OutsideDist <= 1 {
				//					img.Pix[imgOff + 2] = 255
				//				}
				if math.Mod(p.OutsideDist, 5) <= 2.5 {
					img.Pix[imgOff+2] = 255
				}
			}
		}
	}

	return img
}

func (srcField *Field) ScaleDown(factor int) *Field {
	if (srcField.Width%factor) != 0 || (srcField.Height%factor) != 0 {
		panic("invalid size or scale factor")
	}

	dstField := &Field{Width: srcField.Width / factor, Height: srcField.Height / factor}
	dstField.Field = make([]Point, dstField.Width*dstField.Height)

	for y := 0; y < dstField.Height; y++ {
		for x := 0; x < dstField.Width; x++ {

			//			bestOutside := math.MaxFloat64
			//			bestInside := math.MaxFloat64

			sumOutside := 0.0
			sumInside := 0.0

			for sy := 0; sy < factor; sy++ {
				for sx := 0; sx < factor; sx++ {
					soff := (x*factor + sx) + (y*factor+sy)*srcField.Width
					//					if srcField.Field[soff].OutsideDist < bestOutside {
					//						bestOutside = srcField.Field[soff].OutsideDist
					//					}
					//					if srcField.Field[soff].InsideDist < bestInside {
					//						bestInside = srcField.Field[soff].InsideDist
					//					}
					sumOutside += srcField.Field[soff].OutsideDist
					sumInside += srcField.Field[soff].InsideDist
				}
			}

			doff := x + y*dstField.Width
			//			dstField.Field[doff].OutsideDist = bestOutside / float64(factor)
			//			dstField.Field[doff].InsideDist = bestInside / float64(factor)
			dstField.Field[doff].OutsideDist = sumOutside / float64(factor*factor*factor)
			dstField.Field[doff].InsideDist = sumInside / float64(factor*factor*factor)
		}
	}

	return dstField
}

func NewMonochromeFromTreshold(srcImage image.Image, grayThreshold, alphaThreshold int) *image.Gray {
	width := srcImage.Bounds().Dx()
	height := srcImage.Bounds().Dy()

	dstImage := image.NewGray(srcImage.Bounds())

	grayNRGBAThreshold := grayThreshold * ((54 + 184 + 18) * 255) // 255 = because rgb is multiplied with alpha [0;255]
	grayRGBAThreshold := grayThreshold * (54 + 184 + 18)

	switch srcImage := srcImage.(type) {
	case *image.NRGBA:
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				off := (x*4 + y*srcImage.Stride)
				a := int(srcImage.Pix[off+3])
				g := (int(srcImage.Pix[off+0])*54 + int(srcImage.Pix[off+1])*184 + int(srcImage.Pix[off+2])*18) * a

				if a >= alphaThreshold || g >= grayNRGBAThreshold {
					//fmt.Println(a, g, grayNRGBAThreshold)
					dstImage.Pix[x+y*dstImage.Stride] = 255
				}
			}
		}
	case *image.RGBA:
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				off := (x*4 + y*srcImage.Stride)
				a := int(srcImage.Pix[off+3])
				g := int(srcImage.Pix[off+0])*54 + int(srcImage.Pix[off+1])*184 + int(srcImage.Pix[off+2])*18

				if a >= alphaThreshold || g >= grayRGBAThreshold {
					dstImage.Pix[x+y*dstImage.Stride] = 255
				}
			}
		}
	default:
		panic(fmt.Sprintf("implement fallback for %T", srcImage))
	}

	return dstImage
}

////////////////////////////////////////////////////////////////////////////////

type point struct {
	OX, OY  int
	OSqDist int

	IX, IY  int
	ISqDist int
}

var inside = point{OX: 0, OY: 0, OSqDist: 0, IX: 1 << 15, IY: 1 << 15, ISqDist: 1 << 31}
var outside = point{OX: 1 << 15, OY: 1 << 15, OSqDist: 1 << 31, IX: 0, IY: 0, ISqDist: 0}

type field struct {
	Width, Height int
	Field         []point
}

func newField(width, height int) *field {
	df := &field{Width: width, Height: height, Field: make([]point, width*height)}
	for i := 0; i < width*height; i++ {
		df.Field[i] = outside
	}

	return df
}

func newFieldFromImage(image *image.Gray) *field {
	imageWidth := image.Bounds().Dx()
	imageHeight := image.Bounds().Dy()

	field := newField(imageWidth+2, imageHeight+2)

	for y := 0; y < imageHeight; y++ {
		for x := 0; x < imageWidth; x++ {
			if image.Pix[x+y*image.Stride] >= 128 {
				field.Field[(x+1)+(y+1)*field.Width] = inside
			}
		}
	}

	return field
}

func updatePoint(d, s *point, xoff, yoff int) {
	sqDist := (s.OX+xoff)*(s.OX+xoff) + (s.OY+yoff)*(s.OY+yoff)
	if sqDist < d.OSqDist {
		d.OX = s.OX + xoff
		d.OY = s.OY + yoff
		d.OSqDist = sqDist
	}

	sqDistInside := (s.IX+xoff)*(s.IX+xoff) + (s.IY+yoff)*(s.IY+yoff)
	if sqDistInside < d.ISqDist {
		d.IX = s.IX + xoff
		d.IY = s.IY + yoff
		d.ISqDist = sqDistInside
	}
}

func (df *field) fill() {
	for y := 1; y < df.Height-1; y++ {
		for x := 1; x < df.Width-1; x++ {
			updatePoint(&df.Field[x+y*df.Width], &df.Field[(x-1)+y*df.Width], -1, 0)      // left
			updatePoint(&df.Field[x+y*df.Width], &df.Field[x+(y-1)*df.Width], 0, -1)      // above
			updatePoint(&df.Field[x+y*df.Width], &df.Field[(x-1)+(y-1)*df.Width], -1, -1) // above and left
			updatePoint(&df.Field[x+y*df.Width], &df.Field[(x+1)+(y-1)*df.Width], 1, -1)  // above and right
		}
		for x := df.Width - 2; x > 0; x-- {
			updatePoint(&df.Field[x+y*df.Width], &df.Field[(x+1)+y*df.Width], 1, 0) // right
		}
	}

	for y := df.Height - 2; y > 0; y-- {
		for x := df.Width - 2; x > 0; x-- {
			updatePoint(&df.Field[x+y*df.Width], &df.Field[(x+1)+y*df.Width], 1, 0)      // right
			updatePoint(&df.Field[x+y*df.Width], &df.Field[x+(y+1)*df.Width], 0, 1)      // below
			updatePoint(&df.Field[x+y*df.Width], &df.Field[(x+1)+(y+1)*df.Width], 1, 1)  // below and right
			updatePoint(&df.Field[x+y*df.Width], &df.Field[(x-1)+(y+1)*df.Width], -1, 1) // below and left
		}
		for x := 1; x < df.Width-1; x++ {
			updatePoint(&df.Field[x+y*df.Width], &df.Field[(x-1)+y*df.Width], -1, 0) // left
		}
	}
}
