package main
import (
	"fmt"
	_ "image/png"
	"os"
	"github.com/nfnt/resize"
	"image"
	"image/png"
	"image/draw"
	"gopkg.in/alecthomas/kingpin.v2"
	"math"
	"log"
	"image/color"
)

var (
	app = kingpin.New("distancefield", "Generate distancefields from images.")

	grayThresholdFlag = app.Flag("gray", "gray threshold (image color must be larger or equal than this to be \"inside\"").Default("16").Int()
	alphaThresholdFlag = app.Flag("alpha", "alpha threshold (image alpha must be larger or equal than this to be \"inside\"").Default("32").Int()
	oversampleFlag = app.Flag("oversample", "oversample image by this (must be power of 2)").Default("1").Int()
	padFlag = app.Flag("pad", "image will be padded by this amount of pixels on all sides").Default("0").Int()
	sourceImageFlag = app.Flag("source", "source image").Required().File()

	distanceFieldCmd = app.Command("distancefield", "save distancefield as image.")
	distanceFieldCmdModeFlag = distanceFieldCmd.Flag("mode", "").Default("signed").Enum("unsigned", "signed", "signed2")
	distanceFieldCmdScaleFlag = distanceFieldCmd.Flag("scale", "multiply stored distances.").Default("1.0").Float64()
	distanceFieldCmdDestinationImageArg = distanceFieldCmd.Arg("dest", "destination image").Required().String()

	glowCmd = app.Command("glow", "create glow from distancefield.")
	glowCmdColorFlag = glowCmd.Flag("color", "color of glow in hex (ganerated image is single channel grayscale if not set)").Default("-1").Int32()
	glowCmdOuterScaleFlag = glowCmd.Flag("oscale", "multiply outer distances.").Default("16.0").Float64()
	glowCmdInnterScaleFlag = glowCmd.Flag("iscale", "multiply inner distances.").Default("64.0").Float64()
	glowCmdGammaFlag = glowCmd.Flag("gamma", "gamma correction value").Default("2.0").Float64()
	glowCmdDestinationImageArg = glowCmd.Arg("dest", "destination image").Required().String()
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

func main() {
	command := kingpin.MustParse(app.Parse(os.Args[1:]))
	_ = command

	if *oversampleFlag < 1 || *oversampleFlag & (*oversampleFlag - 1) != 0 {
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
		srcImg = resize.Resize(uint(orgWidth * *oversampleFlag), uint(orgHeight * *oversampleFlag), srcImg, resize.MitchellNetravali)
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
					dist := math.Min(field.Field[x + y * field.Width].OutsideDist * *distanceFieldCmdScaleFlag, 255)
					dstImg.Pix[x + y * dstImg.Stride] = uint8(dist)
				}
			}

		case "signed":
			for y := 0; y < field.Height; y++ {
				for x := 0; x < field.Width; x++ {
					idist := field.Field[x + y * field.Width].InsideDist
					odist := field.Field[x + y * field.Width].OutsideDist
					if idist == odist {
						dstImg.Pix[x + y * dstImg.Stride] = 128
					} else if idist < odist {
						dist := math.Max((128 - odist * *distanceFieldCmdScaleFlag), 0)
						dstImg.Pix[x + y * dstImg.Stride] = uint8(dist)
					} else {
						dist := math.Min((128 + idist * *distanceFieldCmdScaleFlag), 255)
						dstImg.Pix[x + y * dstImg.Stride] = uint8(dist)
					}
				}
			}

		case "signed2":
			for y := 0; y < field.Height; y++ {
				for x := 0; x < field.Width; x++ {
					idist := field.Field[x + y * field.Width].InsideDist
					odist := field.Field[x + y * field.Width].OutsideDist
					if idist == odist {
						dstImg.Pix[x + y * dstImg.Stride] = 0
					} else if idist < odist {
						dist := math.Max((-odist * *distanceFieldCmdScaleFlag) - 0.5, -128)
						dstImg.Pix[x + y * dstImg.Stride] = uint8(dist)
					} else {
						dist := math.Min((idist * *distanceFieldCmdScaleFlag) + 0.5, 127)
						dstImg.Pix[x + y * dstImg.Stride] = uint8(int8(dist))
					}
				}
			}
		}

		err := saveImage(dstImg, *distanceFieldCmdDestinationImageArg)
		if err != nil {
			log.Panicf("failed to save %s: %s\n", *distanceFieldCmdDestinationImageArg, err)
			os.Exit(2)
		}

	case glowCmd.FullCommand():
		var dstImg image.Image
		var pixwidth int
		var stride int
		var pix []uint8
		if *glowCmdColorFlag < 0 {
			grayImage :=image.NewGray(image.Rect(0, 0, field.Width, field.Height))
			dstImg = grayImage
			pixwidth = 1
			stride = grayImage.Stride
			pix = grayImage.Pix
		} else {
			colorImg := image.NewNRGBA(image.Rect(0, 0, field.Width, field.Height))
			draw.Over.Draw(colorImg, colorImg.Rect, image.NewUniform(color.RGBA{uint8(*glowCmdColorFlag>>16), (uint8(*glowCmdColorFlag>>8)), uint8(*glowCmdColorFlag), 255}), image.ZP)
			dstImg = colorImg
			pixwidth = 4
			stride = colorImg.Stride
			pix = colorImg.Pix[3:]
		}

		for y := 0; y < field.Height; y++ {
			for x := 0; x < field.Width; x++ {
				idist := field.Field[x + y * field.Width].InsideDist
				odist := field.Field[x + y * field.Width].OutsideDist
				dist := math.Max(idist**glowCmdInnterScaleFlag, odist**glowCmdOuterScaleFlag)
				dist = math.Pow(dist/256, *glowCmdGammaFlag)*256
				dist = math.Min(dist + 0.5, 255)
				pix[x*pixwidth + y * stride] = 255-uint8(dist)
			}
		}

		err := saveImage(dstImg, *glowCmdDestinationImageArg)
		if err != nil {
			log.Panicf("failed to save %s: %s\n", *glowCmdDestinationImageArg, err)
			os.Exit(2)
		}
	}
}
