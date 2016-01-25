package main

import (
	"fmt"
	"github.com/nfnt/resize"
	"gopkg.in/alecthomas/kingpin.v2"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	_ "image/png"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

var (
	app = kingpin.New("distancefield", "Generate distancefields from images.")

	// todo, add line on how colors are formatted "#RRGGBB" or "#AARRGGBB")
	// todo, clean up color parsing (change all color inputs to "#..."

	grayThresholdFlag  = app.Flag("gray", "gray threshold (image color must be larger or equal than this to be \"inside\"").Default("16").Int()
	alphaThresholdFlag = app.Flag("alpha", "alpha threshold (image alpha must be larger or equal than this to be \"inside\"").Default("32").Int()
	oversampleFlag     = app.Flag("oversample", "oversample image by this (must be power of 2)").Default("1").Int()
	padFlag            = app.Flag("pad", "image will be padded by this amount of pixels on all sides").Default("0").Int()
	sourceImageFlag    = app.Flag("source", "source image").Required().File()

	distanceFieldCmd                    = app.Command("distancefield", "save distancefield as image.")
	distanceFieldCmdModeFlag            = distanceFieldCmd.Flag("mode", "").Default("signed").Enum("unsigned", "signed", "signed2")
	distanceFieldCmdScaleFlag           = distanceFieldCmd.Flag("scale", "multiply stored distances.").Default("1.0").Float64()
	distanceFieldCmdDestinationImageArg = distanceFieldCmd.Arg("dest", "destination image").Required().String()

	glowCmd                    = app.Command("glow", "create simple glow from distancefield.")
	glowCmdColorFlag           = glowCmd.Flag("color", "color of glow in hex (\"0xrrggbb\"). If not set dest will be single channel grayscale").Default("-1").Int32()
	glowCmdOuterScaleFlag      = glowCmd.Flag("oscale", "multiply outer distances.").Default("16.0").Float64()
	glowCmdInnterScaleFlag     = glowCmd.Flag("iscale", "multiply inner distances.").Default("64.0").Float64()
	glowCmdGammaFlag           = glowCmd.Flag("gamma", "gamma correction value").Default("2.0").Float64()
	glowCmdDestinationImageArg = glowCmd.Arg("dest", "destination image").Required().String()

	outlineCmd = app.Command("outline", "create colored outline from distancefield.")
	//outlineCmdColorFlag = outlineCmd.Flag("color", "default color of outline in hex (\"0xrrggbb\")").Default("-1").Int32()
	outlineCmdSampleFlag          = outlineCmd.Flag("color", "map dist to color (format: dist:color) use negative dist for inside.").Required().Strings()
	outlineCmdDestinationImageArg = outlineCmd.Arg("dest", "destination image").Required().String()
)

func saveImage(i image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = png.Encode(f, i)
	if err != nil {
		return err
	}

	return nil
}

func parseColor(hexColor string) (col color.NRGBA, err error) {
	if len(hexColor) < 1 || hexColor[0] != '#' {
		err = fmt.Errorf("Invalid color format ('%s'), expected first character to be a '#'", hexColor)
	} else {
		colval64, err := strconv.ParseUint(hexColor[1:], 16, 32)
		if err == nil {
			colval := int(colval64)

			switch len(hexColor) {
			case 4: // #rgb
				col = color.NRGBA{uint8(((colval >> 8) & 0xf) * 0x11), uint8(((colval >> 4) & 0xf) * 0x11), uint8(((colval >> 0) & 0xf) * 0x11), 255}
			case 5: // #argb
				col = color.NRGBA{uint8(((colval >> 8) & 0xf) * 0x11), uint8(((colval >> 4) & 0xf) * 0x11), uint8(((colval >> 0) & 0xf) * 0x11), uint8(((colval >> 12) & 0xf) * 0x11)}
			case 7: // #rrggbb
				col = color.NRGBA{uint8(colval >> 16), uint8(colval >> 8), uint8(colval), 255}
			case 9: // #aarrggbb
				col = color.NRGBA{uint8(colval >> 16), uint8(colval >> 8), uint8(colval), uint8(colval >> 24)}
			default:
				err = fmt.Errorf("Invalid color format ('%s')", hexColor)
			}
		}
	}

	return
}

//type distColor struct{Dist float64; A, R, G, B float64;}
type distColor struct {
	Dist  float64
	A     uint8
	Color color.YCbCr
}
type distColors []distColor

func interpolate(a float64, c1, c2 distColor) distColor {
	a = (a - c1.Dist) / (c2.Dist - c1.Dist)
	return distColor{
		Dist: c1.Dist + (c2.Dist-c1.Dist)*a,
		A:    c1.A + uint8((float64(c2.A)-float64(c1.A))*a),
		Color: color.YCbCr{
			Y:  c1.Color.Y + uint8((float64(c2.Color.Y)-float64(c1.Color.Y))*a),
			Cb: c1.Color.Cb + uint8((float64(c2.Color.Cb)-float64(c1.Color.Cb))*a),
			Cr: c1.Color.Cr + uint8((float64(c2.Color.Cr)-float64(c1.Color.Cr))*a)},
	}
}

func (c distColors) interpolate(dist float64) distColor {
	if dist <= c[0].Dist {
		return c[0]
	}

	for i := 0; i < len(c)-1; i++ {
		if dist <= c[i+1].Dist {
			return interpolate(dist, c[i], c[i+1])
		}
	}

	return c[len(c)-1]
}

func (c distColors) Len() int {
	return len(c)
}

func (c distColors) Less(a, b int) bool {
	return c[a].Dist < c[b].Dist
}

func (c distColors) Swap(a, b int) {
	c[a], c[b] = c[b], c[a]
}

func RGBToYCbCr(rgba color.NRGBA) color.YCbCr {
	y, cb, cr := color.RGBToYCbCr(rgba.R, rgba.G, rgba.B)
	return color.YCbCr{y, cb, cr}
}

func makeOutline(field *Field, unparsedColors []string, destImagePath string) error {
	colors := make(distColors, len(unparsedColors))
	for i := 0; i < len(unparsedColors); i++ {
		s := strings.Split(unparsedColors[i], ":")

		if len(s) != 2 {
			return fmt.Errorf("Invalid dist:color format ('%s')", unparsedColors[i])
		}

		dist, err := strconv.ParseFloat(s[0], 64)
		if err != nil {
			return err
		}

		rgba, err := parseColor(s[1])
		if err != nil {
			return err
		}

		colors[i] = distColor{dist, rgba.A, RGBToYCbCr(rgba)}
	}

	sort.Sort(colors)
	//	fmt.Println(colors)
	//	for d := -10.0; d<=20; d+= 1 {
	//		fmt.Println(d, colors.interpolate(d))
	//	}

	dstImg := image.NewNRGBA(image.Rect(0, 0, field.Width, field.Height))

	for y := 0; y < field.Height; y++ {
		for x := 0; x < field.Width; x++ {
			idist := field.Field[x+y*field.Width].InsideDist
			odist := field.Field[x+y*field.Width].OutsideDist

			var ycbcr distColor
			if idist >= odist {
				ycbcr = colors.interpolate(-idist)
			} else {
				ycbcr = colors.interpolate(odist)
			}

			r, g, b := color.YCbCrToRGB(ycbcr.Color.Y, ycbcr.Color.Cb, ycbcr.Color.Cr)

			off := x*4 + y*dstImg.Stride
			dstImg.Pix[off] = r
			dstImg.Pix[off+1] = g
			dstImg.Pix[off+2] = b
			dstImg.Pix[off+3] = ycbcr.A
		}
	}

	err := saveImage(dstImg, destImagePath)
	return err
}

func main() {
	command := kingpin.MustParse(app.Parse(os.Args[1:]))
	_ = command

	if *oversampleFlag < 1 || *oversampleFlag&(*oversampleFlag-1) != 0 {
		fmt.Fprintln(os.Stderr, "invalid oversample option")
		os.Exit(1)
	}

	srcImg, _, err := image.Decode(*sourceImageFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to read image", err)
		os.Exit(2)
	}

	orgWidth := srcImg.Bounds().Dx()
	orgHeight := srcImg.Bounds().Dy()

	if *oversampleFlag > 1 {
		srcImg = resize.Resize(uint(orgWidth**oversampleFlag), uint(orgHeight**oversampleFlag), srcImg, resize.MitchellNetravali)
	}

	monoImage := NewMonochromeFromTreshold(srcImg, *grayThresholdFlag, *alphaThresholdFlag)

	if *padFlag > 0 {
		scaledPadSize := *padFlag * *oversampleFlag
		paddedMonoImage := image.NewGray(monoImage.Bounds().Inset(-scaledPadSize))
		draw.Draw(paddedMonoImage, paddedMonoImage.Bounds(), monoImage, image.Pt(-scaledPadSize, -scaledPadSize), draw.Src)
		monoImage = paddedMonoImage
	}

	field := NewFieldFromImage(monoImage)
	if *oversampleFlag > 1 {
		field = field.ScaleDown(*oversampleFlag)
	}

	switch command {
	case distanceFieldCmd.FullCommand():
		dstImg := image.NewGray(image.Rect(0, 0, field.Width, field.Height))

		switch *distanceFieldCmdModeFlag {
		case "unsigned":
			for y := 0; y < field.Height; y++ {
				for x := 0; x < field.Width; x++ {
					dist := math.Min(field.Field[x+y*field.Width].OutsideDist**distanceFieldCmdScaleFlag, 255)
					dstImg.Pix[x+y*dstImg.Stride] = uint8(dist)
				}
			}

		case "signed":
			for y := 0; y < field.Height; y++ {
				for x := 0; x < field.Width; x++ {
					idist := field.Field[x+y*field.Width].InsideDist
					odist := field.Field[x+y*field.Width].OutsideDist
					if idist == odist {
						dstImg.Pix[x+y*dstImg.Stride] = 128
					} else if idist < odist {
						dist := math.Max((128 - odist**distanceFieldCmdScaleFlag), 0)
						dstImg.Pix[x+y*dstImg.Stride] = uint8(dist)
					} else {
						dist := math.Min((128 + idist**distanceFieldCmdScaleFlag), 255)
						dstImg.Pix[x+y*dstImg.Stride] = uint8(dist)
					}
				}
			}

		case "signed2":
			for y := 0; y < field.Height; y++ {
				for x := 0; x < field.Width; x++ {
					idist := field.Field[x+y*field.Width].InsideDist
					odist := field.Field[x+y*field.Width].OutsideDist
					if idist == odist {
						dstImg.Pix[x+y*dstImg.Stride] = 0
					} else if idist < odist {
						dist := math.Max((-odist**distanceFieldCmdScaleFlag)-0.5, -128)
						dstImg.Pix[x+y*dstImg.Stride] = uint8(dist)
					} else {
						dist := math.Min((idist**distanceFieldCmdScaleFlag)+0.5, 127)
						dstImg.Pix[x+y*dstImg.Stride] = uint8(int8(dist))
					}
				}
			}
		}

		err := saveImage(dstImg, *distanceFieldCmdDestinationImageArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to save %s: %s\n", *distanceFieldCmdDestinationImageArg, err)
			os.Exit(2)
		}

	case glowCmd.FullCommand():
		var dstImg image.Image
		var pixwidth int
		var stride int
		var pix []uint8
		if *glowCmdColorFlag < 0 {
			grayImage := image.NewGray(image.Rect(0, 0, field.Width, field.Height))
			dstImg = grayImage
			pixwidth = 1
			stride = grayImage.Stride
			pix = grayImage.Pix
		} else {
			colorImg := image.NewNRGBA(image.Rect(0, 0, field.Width, field.Height))
			draw.Over.Draw(colorImg, colorImg.Rect, image.NewUniform(color.RGBA{uint8(*glowCmdColorFlag >> 16), (uint8(*glowCmdColorFlag >> 8)), uint8(*glowCmdColorFlag), 255}), image.ZP)
			dstImg = colorImg
			pixwidth = 4
			stride = colorImg.Stride
			pix = colorImg.Pix[3:]
		}

		for y := 0; y < field.Height; y++ {
			for x := 0; x < field.Width; x++ {
				idist := field.Field[x+y*field.Width].InsideDist
				odist := field.Field[x+y*field.Width].OutsideDist
				dist := math.Max(idist**glowCmdInnterScaleFlag, odist**glowCmdOuterScaleFlag)
				dist = math.Pow(dist/256, *glowCmdGammaFlag) * 256
				dist = math.Min(dist+0.5, 255)
				pix[x*pixwidth+y*stride] = 255 - uint8(dist)
			}
		}

		err := saveImage(dstImg, *glowCmdDestinationImageArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to save %s: %s\n", *glowCmdDestinationImageArg, err)
			os.Exit(2)
		}

	case outlineCmd.FullCommand():
		err = makeOutline(field, *outlineCmdSampleFlag, *outlineCmdDestinationImageArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to generate outline: %s\n", err)
			os.Exit(2)
		}
	}
}
