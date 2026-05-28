#!/usr/bin/env python3
import sys
import json
import numpy as np
import librosa

def detect_key(y, sr):
    chroma = librosa.feature.chroma_cqt(y=y, sr=sr)
    chroma_mean = np.mean(chroma, axis=1)

    major_profile = np.array([6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88])
    minor_profile = np.array([6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17])
    notes = ['C', 'C#', 'D', 'Eb', 'E', 'F', 'F#', 'G', 'Ab', 'A', 'Bb', 'B']

    best_corr = -np.inf
    best_key = 'C major'
    for i in range(12):
        corr_major = np.corrcoef(chroma_mean, np.roll(major_profile, i))[0, 1]
        corr_minor = np.corrcoef(chroma_mean, np.roll(minor_profile, i))[0, 1]
        if corr_major > best_corr:
            best_corr = corr_major
            best_key = f'{notes[i]} major'
        if corr_minor > best_corr:
            best_corr = corr_minor
            best_key = f'{notes[i]} minor'
    return best_key

audio_file = sys.argv[1]
y, sr = librosa.load(audio_file, mono=True)

tempo, _ = librosa.beat.beat_track(y=y, sr=sr)
bpm = float(tempo[0]) if hasattr(tempo, '__len__') else float(tempo)
bpm = round(bpm, 1)

key = detect_key(y, sr)

print(json.dumps({'bpm': bpm, 'key': key}))
