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
	"os"
	"strings"
	"unicode"

	"github.com/maruel/ask/internal"
	"github.com/maruel/genai"
	"github.com/maruel/genai/gemini"
	"golang.org/x/sync/errgroup"
)

const systemPrompt = `**Generate simple, animated doodle GIFs on white from user input, prioritizing key visual identifiers in an animated doodle style with ethical considerations.**
**Core GIF:** Doodle/cartoonish (simple lines, stylized forms, no photorealism), subtle looping motion (primary subject(s) only: wiggle, shimmer, etc.), white background, lighthearted/positive tone (playful, avoids trivializing serious subjects), uses specified colors (unless monochrome/outline requested).
**Input Analysis:** Identify subject (type, specificity), prioritize visual attributes (hair C/T, skin tone neutrally if discernible/needed, clothes C/P, accessories C, facial hair type, other distinct features neutrally for people; breed, fur C/P for animals; key parts, colors for objects), extract text (content, style hints described, display as requested: speech bubble [format: 'Speech bubble says "[Text]" is persistent.'], caption/title [format: 'with the [title/caption] "[Text]" [position]'], or text-as-subject [format: 'the word "[Text]" in [style/color description]']), note style modifiers (e.g., "pencil sketch," "monochrome"), and action (usually "subtle motion"). If the subject or description is too vague, add specific characteristics to make it more unique and detailed.
**Prompt Template:** "[Style Descriptor(s)] [Subject Description with Specificity, Attributes, Colors, Skin Tone if applicable] [Text Component if applicable and NOT speech bubble]. [Speech Bubble Component if applicable]"
**Template Notes:** '[Style Descriptor(s)]' includes "cartoonish" or "doodle style" (especially for people) plus any user-requested modifiers. '[Subject Description...]' combines all relevant subject and attribute details. '[Text Component...]' is for captions, titles, or text-as-subject only. '[Speech Bubble Component...]' is for speech bubbles only (mutually exclusive with Text Component).
**Key Constraints:** No racial labels. Neutral skin tone descriptors when included. Cartoonish/doodle style always implied, especially for people. One text display method only.
`

func runSync(ctx context.Context, c *gemini.Client, msgs genai.Messages, opts genai.Validatable) (genai.Messages, error) {
	resp, err := c.Chat(ctx, msgs, opts)
	if err != nil {
		return nil, err
	}
	return genai.Messages{resp.Message}, err
}

func runAsync(ctx context.Context, c *gemini.Client, msgs genai.Messages, opts genai.Validatable) (genai.Messages, error) {
	chunks := make(chan genai.MessageFragment)
	ch := make(chan genai.Messages)
	eg := errgroup.Group{}
	eg.Go(func() error {
		var m genai.Messages
		hasLF := false
		start := true
		defer func() {
			if !hasLF {
				_, _ = os.Stdout.WriteString("\n")
			}
			ch <- m
		}()
		for {
			select {
			case <-ctx.Done():
				return nil
			case pkt, ok := <-chunks:
				if !ok {
					return nil
				}
				// Everytime a new Content is generated, the previous one could be shown.
				var err error
				m, err = pkt.Accumulate(m)
				if err != nil {
					return err
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

	_, err2 := c.ChatStream(ctx, msgs, opts, chunks)
	close(chunks)
	msg := <-ch
	if err3 := eg.Wait(); err2 == nil {
		err2 = err3
	}
	return msg, err2
}

func run(ctx context.Context, query, filename string) error {
	cBase, err := gemini.New("", "gemini-2.0-flash")
	if err != nil {
		return err
	}
	fmt.Printf("Generating prompt...\n")
	msgs := genai.Messages{genai.NewTextMessage(genai.User, query)}
	opts := gemini.ChatOptions{
		ChatOptions: genai.ChatOptions{
			SystemPrompt: systemPrompt,
			Temperature:  1,
			Seed:         1,
		},
	}
	msgs, err = runSync(ctx, cBase, msgs, &opts)
	if err != nil {
		return err
	}
	if len(msgs) != 1 {
		return fmt.Errorf("expected one message, got %d", len(msgs))
	}
	processed := msgs[0].AsText()
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
		genai.NewTextMessage(genai.User, contents),
	}
	opts = gemini.ChatOptions{
		ResponseModalities: []gemini.Modality{gemini.ModalityText, gemini.ModalityImage},
		ChatOptions: genai.ChatOptions{
			Temperature: 1,
			Seed:        1,
		},
	}
	// Doesn't support image generation yet?
	// const model = "gemini-2.5-flash-preview-05-20"
	cImg, err := gemini.New("", "gemini-2.0-flash-preview-image-generation")
	if err != nil {
		return err
	}
	msgs, err = runAsync(ctx, cImg, msgs, &opts)
	if err != nil {
		return err
	}
	var imgs []image.Image
	for _, msg := range msgs {
		index := 0
		for _, c := range msg.Contents {
			if strings.TrimSpace(c.Text) != "" {
				fmt.Printf("%s\n", c.Text)
			} else if c.Thinking != "" {
				fmt.Printf("%s\n", c.Thinking)
			} else if c.Document != nil {
				if !strings.HasSuffix(c.Filename, ".png") {
					fmt.Printf("Unexpected file %q\n", c.Filename)
					continue
				}
				img, err := png.Decode(c.Document)
				if err != nil {
					return err
				}
				imgs = append(imgs, img)
				name := fmt.Sprintf("content%d.png", index)
				index++
				fmt.Printf("Creating %s\n", name)
				f, err := os.Create(name)
				if err != nil {
					return err
				}
				c.Document.Seek(0, 0)
				_, err = io.Copy(f, c.Document)
				_ = f.Close()
				if err != nil {
					return err
				}
			} else if c.URL != "" {
				fmt.Printf("URL: %s\n", c.URL)
			}
		}
	}
	if len(imgs) == 0 {
		return nil
	}
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
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return gif.EncodeAll(f, &g)
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
