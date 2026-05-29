import sys
import json
import whisper

def main():
    if len(sys.argv) < 2:
        print(json.dumps({"error": "no file provided"}))
        sys.exit(1)

    audio_file = sys.argv[1]
    model_name = sys.argv[2] if len(sys.argv) > 2 else "base"

    model = whisper.load_model(model_name)
    result = model.transcribe(audio_file, fp16=False)
    print(json.dumps({"text": result["text"].strip()}))

if __name__ == "__main__":
    main()
