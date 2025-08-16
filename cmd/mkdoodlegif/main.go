// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"io"
	"log/slog"
	"math"
	"os"
	"strings"
	"unicode"

	"github.com/maruel/ask/internal"
	"github.com/maruel/genai"
	"github.com/maruel/genai/providers/gemini"
	"golang.org/x/sync/errgroup"
)

const systemPrompt = `**Generate simple, animated doodle GIFs on white from user input, prioritizing key visual identifiers in an animated doodle style with ethical considerations.**
**Core GIF:** Doodle/cartoonish (simple lines, stylized forms, no photorealism), subtle looping motion (primary subject(s) only: wiggle, shimmer, etc.), white background, lighthearted/positive tone (playful, avoids trivializing serious subjects), uses specified colors (unless monochrome/outline requested).
**Input Analysis:** Identify subject (type, specificity), prioritize visual attributes (hair C/T, skin tone neutrally if discernible/needed, clothes C/P, accessories C, facial hair type, other distinct features neutrally for people; breed, fur C/P for animals; key parts, colors for objects), extract text (content, style hints described, display as requested: speech bubble [format: 'Speech bubble says "[Text]" is persistent.'], caption/title [format: 'with the [title/caption] "[Text]" [position]'], or text-as-subject [format: 'the word "[Text]" in [style/color description]']), note style modifiers (e.g., "pencil sketch," "monochrome"), and action (usually "subtle motion"). If the subject or description is too vague, add specific characteristics to make it more unique and detailed.
**Prompt Template:** "[Style Descriptor(s)] [Subject Description with Specificity, Attributes, Colors, Skin Tone if applicable] [Text Component if applicable and NOT speech bubble]. [Speech Bubble Component if applicable]"
**Template Notes:** '[Style Descriptor(s)]' includes "cartoonish" or "doodle style" (especially for people) plus any user-requested modifiers. '[Subject Description...]' combines all relevant subject and attribute details. '[Text Component...]' is for captions, titles, or text-as-subject only. '[Speech Bubble Component...]' is for speech bubbles only (mutually exclusive with Text Component).
**Key Constraints:** No racial labels. Neutral skin tone descriptors when included. Cartoonish/doodle style always implied, especially for people. One text display method only.
`

func runSync(ctx context.Context, c *gemini.Client, msgs genai.Messages, opts genai.Options) (genai.Message, error) {
	resp, err := c.GenSync(ctx, msgs, opts)
	return resp.Message, err
}

func runAsync(ctx context.Context, c *gemini.Client, msgs genai.Messages, opts genai.Options) (genai.Message, error) {
	chunks := make(chan genai.ReplyFragment)
	eg := errgroup.Group{}
	eg.Go(func() error {
		hasLF := false
		start := true
		defer func() {
			if !hasLF {
				_, _ = os.Stdout.WriteString("\n")
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return nil
			case pkt, ok := <-chunks:
				if !ok {
					return nil
				}
				if start {
					pkt.TextFragment = strings.TrimLeftFunc(pkt.TextFragment, unicode.IsSpace)
					start = false
				}
				if pkt.TextFragment != "" {
					hasLF = strings.ContainsRune(pkt.TextFragment, '\n')
				} else if pkt.Filename != "" {
					fmt.Printf("Got %s..\n", pkt.Filename)
				}
				_, _ = os.Stdout.WriteString(pkt.TextFragment)
			}
		}
	})

	resp, err2 := c.GenStream(ctx, msgs, chunks, opts)
	close(chunks)
	if err3 := eg.Wait(); err2 == nil {
		err2 = err3
	}
	return resp.Message, err2
}

func run(ctx context.Context, query, filename string) error {
	cBase, err := gemini.New(&genai.OptionsProvider{Model: "gemini-2.5-flash"}, nil)
	if err != nil {
		return err
	}
	fmt.Printf("Generating prompt...\n")
	msgs := genai.Messages{genai.NewTextMessage(query)}
	opts := gemini.Options{
		OptionsText: genai.OptionsText{
			SystemPrompt: systemPrompt,
			Temperature:  1,
			Seed:         1,
		},
		ResponseModalities: genai.Modalities{genai.ModalityText},
	}
	msg, err := runSync(ctx, cBase, msgs, &opts)
	if err != nil {
		return err
	}
	processed := msg.AsText()
	fmt.Printf("Prompt is: %s\n", processed)
	fmt.Printf("Generating images...\n")
	prompt := `A doodle animation on a white background of ` + processed + `. Subtle motion but nothing else moves.`
	style := `Simple, vibrant, varied-colored doodle/hand-drawn sketch`
	contents := `Generate at least 10 square, white-background doodle animation frames with smooth, fluid, vibrantly colored motion depicting ` + prompt + `.

		*Mandatory Requirements (Compacted):**

		**Style:** ` + style + `.
		**Background:** Plain solid white (no background colors/elements). Absolutely no black background.
		**Content & Motion:** Clearly depict **` + prompt + `** action with colored, moving subject (no static images). If there's an action specified, it should be the main difference between frames.
		**Frame Count:** At least 5 frames showing continuous progression and at most 10 frames.
		**Format:** Square image (1:1 aspect ratio).
		**Cropping:** Absolutely no black bars/letterboxing; colorful doodle fully visible against white.
		**Output:** Actual image files for a smooth, colorful doodle-style GIF on a white background. Make sure every frame is different enough from the previous one.`

	msgs = genai.Messages{
		genai.NewTextMessage(contents),
	}
	opts = gemini.Options{
		OptionsText: genai.OptionsText{
			Temperature: 1,
			Seed:        1,
		},
		ResponseModalities: genai.Modalities{genai.ModalityText, genai.ModalityImage},
	}
	// As of 2025-08-10, "gemini-2.5-flash" doesn't support image generation yet in Canada.
	// "gemini-2.0-flash-image-generation" was removed for a few days but got added back to the model list. But
	// when I try it, the API replies the model doesn't exist.
	cImg, err := gemini.New(&genai.OptionsProvider{Model: "gemini-2.5-flash"}, nil)
	if err != nil {
		return err
	}
	msg, err = runAsync(ctx, cImg, msgs, &opts)
	if err != nil {
		return err
	}
	var imgs []image.Image
	index := 0
	for _, r := range msg.Replies {
		if r.Text != "" {
			if strings.TrimSpace(r.Text) != "" {
				fmt.Printf("%s\n", r.Text)
			}
		} else if r.Thinking != "" {
			fmt.Printf("%s\n", r.Thinking)
		} else if r.Doc.Src != nil {
			n := r.Doc.GetFilename()
			if !strings.HasSuffix(n, ".png") {
				fmt.Printf("Unexpected file %q\n", n)
				continue
			}
			img, err2 := png.Decode(r.Doc.Src)
			if err2 != nil {
				return err2
			}
			imgs = append(imgs, img)
			name := fmt.Sprintf("content%d.png", index)
			index++
			fmt.Printf("Creating %s\n", name)
			f, err2 := os.Create(name)
			if err2 != nil {
				return err2
			}
			_, _ = r.Doc.Src.Seek(0, 0)
			_, err = io.Copy(f, r.Doc.Src)
			_ = f.Close()
			if err != nil {
				return err
			}
		} else if r.Doc.URL != "" {
			fmt.Printf("URL: %s\n", r.Doc.URL)
		} else {
			return fmt.Errorf("unexpected content: %+v", r)
		}
	}
	if len(imgs) == 0 {
		return nil
	}
	imgs = trimImages(imgs)
	// Accumulate the images, save as a GIF.
	g := gif.GIF{
		Config: image.Config{
			ColorModel: color.Palette(palette.Plan9),
			Width:      imgs[0].Bounds().Dx(),
			Height:     imgs[0].Bounds().Dy(),
		},
	}
	for i := range imgs {
		b := imgs[i].Bounds()
		pm := image.NewPaletted(b, palette.Plan9)
		draw.FloydSteinberg.Draw(pm, b, imgs[i], b.Min)
		g.Image = append(g.Image, pm)
		g.Delay = append(g.Delay, 100)
	}
	fmt.Printf("Creating %s\n", filename)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return gif.EncodeAll(f, &g)
}

// trimImages detects borders on all sides and trims them.
// It may change the aspect ratio a little.
func trimImages(imgs []image.Image) []image.Image {
	if len(imgs) == 0 {
		return imgs
	}

	// Find the maximum possible trimming for each edge
	maxTop, maxLeft, maxRight, maxBottom := math.MaxInt, math.MaxInt, math.MaxInt, math.MaxInt
	for _, img := range imgs {
		bounds := img.Bounds()
		width, height := bounds.Dx(), bounds.Dy()

		// Find top uniform color edge
		top := 0
		for y := bounds.Min.Y; y < bounds.Min.Y+height; y++ {
			edgeColor := img.At(bounds.Min.X, y)
			uniform := true
			for x := bounds.Min.X; x < bounds.Min.X+width; x++ {
				if !colorEqual(img.At(x, y), edgeColor) {
					uniform = false
					break
				}
			}
			if !uniform {
				top = y - bounds.Min.Y
				break
			}
		}
		if top < maxTop {
			maxTop = top
		}

		// Find left uniform color edge
		left := 0
		for x := bounds.Min.X; x < bounds.Min.X+width; x++ {
			edgeColor := img.At(x, bounds.Min.Y)
			uniform := true
			for y := bounds.Min.Y; y < bounds.Min.Y+height; y++ {
				if !colorEqual(img.At(x, y), edgeColor) {
					uniform = false
					break
				}
			}
			if !uniform {
				left = x - bounds.Min.X
				break
			}
		}
		if left < maxLeft {
			maxLeft = left
		}

		// Find right uniform color edge
		right := 0
		for x := bounds.Max.X - 1; x >= bounds.Min.X; x-- {
			edgeColor := img.At(x, bounds.Min.Y)
			uniform := true
			for y := bounds.Min.Y; y < bounds.Min.Y+height; y++ {
				if !colorEqual(img.At(x, y), edgeColor) {
					uniform = false
					break
				}
			}
			if !uniform {
				right = bounds.Max.X - 1 - x
				break
			}
		}
		if right < maxRight {
			maxRight = right
		}

		// Find bottom uniform color edge
		bottom := 0
		for y := bounds.Max.Y - 1; y >= bounds.Min.Y; y-- {
			edgeColor := img.At(bounds.Min.X, y)
			uniform := true
			for x := bounds.Min.X; x < bounds.Min.X+width; x++ {
				if !colorEqual(img.At(x, y), edgeColor) {
					uniform = false
					break
				}
			}
			if !uniform {
				bottom = bounds.Max.Y - 1 - y
				break
			}
		}
		if bottom < maxBottom {
			maxBottom = bottom
		}
	}

	// If no uniform borders found, return original images
	if maxTop == 0 && maxLeft == 0 && maxRight == 0 && maxBottom == 0 {
		return imgs
	}
	// Trim all images by the common amount
	trimmedImgs := make([]image.Image, len(imgs))
	for i, img := range imgs {
		bounds := img.Bounds()
		// Calculate new bounds
		newBounds := image.Rect(
			bounds.Min.X+maxLeft,
			bounds.Min.Y+maxTop,
			bounds.Max.X-maxRight,
			bounds.Max.Y-maxBottom,
		)
		// Create a new image with the trimmed dimensions
		trimmed := image.NewNRGBA(image.Rect(0, 0, newBounds.Dx(), newBounds.Dy()))
		draw.Draw(trimmed, trimmed.Bounds(), img, newBounds.Min, draw.Src)
		trimmedImgs[i] = trimmed
	}
	return trimmedImgs
}

// colorEqual checks if two colors are equal by comparing their RGBA values.
func colorEqual(c1, c2 color.Color) bool {
	r1, g1, b1, a1 := c1.RGBA()
	r2, g2, b2, a2 := c2.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

func mainImpl() error {
	ctx, stop := internal.Init()
	defer stop()

	verbose := flag.Bool("v", false, "verbose")
	filename := flag.String("out", "doodle.gif", "result file")
	flag.Parse()
	if flag.NArg() != 1 {
		return errors.New("ask something to doodle, e.g. \"a shiba inu eating ice-cream\"")
	}
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
	}
	query := flag.Arg(0)
	return run(ctx, query, *filename)
}

func main() {
	if err := mainImpl(); err != nil {
		if err != context.Canceled {
			fmt.Fprintf(os.Stderr, "mkdoodlegif: %s\n", err)
		}
		os.Exit(1)
	}
}
