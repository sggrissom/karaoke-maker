#!/usr/bin/env python3
import sys
import json
import numpy as np
import librosa

def midi_to_note(midi):
    notes = ['C', 'C#', 'D', 'Eb', 'E', 'F', 'F#', 'G', 'Ab', 'A', 'Bb', 'B']
    octave = (int(midi) // 12) - 1
    note = notes[int(midi) % 12]
    return f"{note}{octave}"

def classify_voice(mid_midi):
    if mid_midi >= 65:
        return "Soprano"
    elif mid_midi >= 62:
        return "Mezzo-soprano"
    elif mid_midi >= 57:
        return "Alto"
    elif mid_midi >= 52:
        return "Tenor"
    elif mid_midi >= 49:
        return "Baritone"
    else:
        return "Bass"

audio_file = sys.argv[1]

y, sr = librosa.load(audio_file, mono=True, duration=90)

f0, voiced_flag, _ = librosa.pyin(
    y,
    fmin=librosa.note_to_hz('C2'),
    fmax=librosa.note_to_hz('C7'),
    sr=sr,
)

voiced_f0 = f0[voiced_flag & ~np.isnan(f0)]

if len(voiced_f0) < 10:
    print(json.dumps({'range': ''}))
    sys.exit(0)

midi_values = librosa.hz_to_midi(voiced_f0)

low_midi = int(round(float(np.percentile(midi_values, 5))))
high_midi = int(round(float(np.percentile(midi_values, 95))))
mid_midi = (low_midi + high_midi) / 2

low_note = midi_to_note(low_midi)
high_note = midi_to_note(high_midi)
voice_type = classify_voice(mid_midi)

print(json.dumps({'range': f'{low_note}–{high_note} ({voice_type})'}))
