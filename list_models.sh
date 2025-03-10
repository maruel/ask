#!/usr/bin/env bash
# Copyright 2025 Marc-Antoine Ruel. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

set -eu

go install ./cmd/list-models
echo "Anthropic:"
list-models -provider anthropic | sed 's/^/- /'
echo "Cohere:"
list-models -provider cohere | sed 's/^/- /'
echo "DeepSeek:"
list-models -provider deepseek | sed 's/^/- /'
echo "Gemini:"
list-models -provider gemini | sed 's/^/- /'
echo "Groq:"
list-models -provider groq | sed 's/^/- /'
echo "Mistral:"
list-models -provider mistral | sed 's/^/- /'
echo "OpenAI:"
list-models -provider openai | sed 's/^/- /'
