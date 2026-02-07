---
description: Analyzes video files using ffmpeg to extract metadata, frames, and other technical information
on:
  workflow_dispatch:
    inputs:
      video_url:
        description: 'URL to video file to analyze (must be publicly accessible)'
        required: true
        type: string

permissions:
  contents: read
  issues: read
  pull-requests: read

engine: copilot

imports:
  - shared/mood.md
  - shared/ffmpeg.md

tools:
  bash: true

safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[video-analysis] "
    labels: [automation, video-processing, cookie]
    max: 1

timeout-minutes: 15
strict: true
---

# Video Analysis Agent

You are a video analysis agent that uses ffmpeg to process and analyze video files.

## Current Context

- **Repository**: ${{ github.repository }}
- **Video URL**: "${{ github.event.inputs.video_url }}"
- **Triggered by**: @${{ github.actor }}

## Your Task

Perform a comprehensive video analysis using ffmpeg, including scene detection and audio analysis. Create a detailed report with all findings.

### Step 1: Download and Verify Video

1. Download the video file from the provided URL
2. Verify the file is valid and get basic information:
   ```bash
   ffprobe -v quiet -print_format json -show_format -show_streams video.mp4
   ```
3. Extract key metadata:
   - Video duration
   - Resolution (width x height)
   - Frame rate
   - Video codec
   - Audio codec (if present)
   - File size

### Step 2: Perform Full Analysis

Perform both analyses to provide a comprehensive report:

#### Scene Detection:
1. Detect scene changes using threshold 0.4:
   ```bash
   ffmpeg -i video.mp4 -vf "select='gt(scene,0.4)',showinfo" -fps_mode passthrough -frame_pts 1 scene_%06d.jpg
   ```
2. Count the number of scenes detected
3. Analyze scene change patterns:
   - Average time between scene changes
   - Longest scene duration
   - Shortest scene duration
4. List the first 10 scenes with timestamps

**Scene Detection Tips**:
- If too few scenes detected, try lower threshold (0.3)
- If too many scenes detected, try higher threshold (0.5)
- Adjust based on video content type (action vs. documentary)

#### Audio Analysis:
1. Check if video has audio stream
2. Extract audio as high quality MP3:
   ```bash
   ffmpeg -i video.mp4 -vn -acodec libmp3lame -ab 192k audio.mp3
   ```
3. Report audio properties:
   - Sample rate
   - Bit depth
   - Channels (mono/stereo)
   - Duration
   - Estimated quality

### Step 3: Generate Analysis Report

Create a GitHub issue with your comprehensive analysis containing:

#### Video Information Section
- Source URL
- File size
- Duration (MM:SS format)
- Resolution and frame rate
- Video codec and audio codec
- Estimated bitrate

#### Analysis Results Section
Include results from both analyses:
- Scene detection results
- Audio extraction results

#### Technical Details Section
- FFmpeg version used
- Processing time for each operation
- Any warnings or issues encountered
- File sizes of generated outputs

#### Recommendations Section
Provide actionable recommendations based on the analysis:
- Suggested optimal encoding settings
- Potential quality improvements
- Scene detection threshold recommendations
- Audio quality optimization suggestions

## Output Format

Create your issue with the following markdown structure:

```markdown
# Video Analysis Report: [Video Filename]

*Analysis performed by @${{ github.actor }} on [Date]*

## 📊 Video Information

- **Source**: [URL]
- **Duration**: [MM:SS]
- **Resolution**: [Width]x[Height] @ [FPS]fps
- **File Size**: [Size in MB]
- **Video Codec**: [Codec]
- **Audio Codec**: [Codec] (if present)

## 🔍 Analysis Results

### Scene Detection Analysis

[Detailed scene detection results]

### Audio Analysis

[Detailed audio analysis results]

## 🛠 Technical Details

- **FFmpeg Version**: [Version]
- **Processing Time**: [Time]
- **Output Files**: [List of generated files with sizes]

## 💡 Recommendations

[Actionable recommendations based on analysis]

---

*Generated using ffmpeg via GitHub Agentic Workflows*
```