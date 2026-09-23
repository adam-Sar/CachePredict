"""Convert the trained Keras model to ONNX for Go inference.

Tries the direct Keras -> ONNX path first. If tf2onnx rejects the
Keras 3 .keras format, falls back to SavedModel -> ONNX.
"""
import os
import sys
import tensorflow as tf
import tf2onnx

KERAS_PATH = "cache_predict_model.keras"
ONNX_PATH = "cache_predict_model.onnx"
SAVED_MODEL_DIR = "cache_predict_savedmodel"
MAX_LEN = 8  # must match model.ipynb

print(f"loading {KERAS_PATH} ...")
model = tf.keras.models.load_model(KERAS_PATH)
print(f"  input shape:  {model.input_shape}")
print(f"  output shape: {model.output_shape}")

spec = (tf.TensorSpec((None, MAX_LEN), tf.int32, name="input"),)


def try_direct_keras():
    print("\nattempt 1: Keras -> ONNX directly ...")
    tf2onnx.convert.from_keras(
        model,
        input_signature=spec,
        output_path=ONNX_PATH,
        opset=13,
    )
    return True


def try_via_savedmodel():
    print("\nattempt 2: via SavedModel ...")
    # clean previous
    import shutil
    if os.path.isdir(SAVED_MODEL_DIR):
        shutil.rmtree(SAVED_MODEL_DIR)

    # Keras 3: use model.export(). Falls back to tf.saved_model.save() on older.
    if hasattr(model, "export"):
        model.export(SAVED_MODEL_DIR)
    else:
        tf.saved_model.save(model, SAVED_MODEL_DIR)

    # shell out to the CLI — it handles SavedModel cleanly
    rc = os.system(
        f'python -m tf2onnx.convert '
        f'--saved-model "{SAVED_MODEL_DIR}" '
        f'--output "{ONNX_PATH}" '
        f'--opset 13'
    )
    return rc == 0


ok = False
try:
    ok = try_direct_keras()
except Exception as e:
    print(f"  direct path failed: {type(e).__name__}: {e}")

if not ok:
    ok = try_via_savedmodel()

if ok and os.path.exists(ONNX_PATH):
    size = os.path.getsize(ONNX_PATH)
    print(f"\n✅ saved {ONNX_PATH}  ({size:,} bytes)")
else:
    print("\n❌ conversion failed", file=sys.stderr)
    sys.exit(1)


# ---------------------------------------------------------------------------
# Save the tokenizer (LabelEncoder) so Go can encode API call strings -> ids
# ---------------------------------------------------------------------------
from sklearn.preprocessing import LabelEncoder

print("\nsaving tokenizer ...")
csv = "synthetic_api_calls.csv"
df = tf.keras.utils.get_file("synthetic_api_calls.csv", "") if False else None
import pandas as pd
df = pd.read_csv(csv)
df["url_params"] = df["url_params"].fillna("")
df["body"] = df["body"].fillna("")

all_calls = pd.concat([df["current_call"], df["next_call"]]).unique()
le = LabelEncoder().fit(all_calls)

tokenizer = {
    "max_len": MAX_LEN,
    "vocab_size": int(len(le.classes_)),
    "pad_value": 0,
    "string_to_id": {s: int(i) for i, s in enumerate(le.classes_)},
    "id_to_string": {int(i): s for i, s in enumerate(le.classes_)},
    "unk_token": "UNK",
}

import json
with open("tokenizer.json", "w", encoding="utf-8") as f:
    json.dump(tokenizer, f, indent=2, ensure_ascii=False)
print(f"✅ saved tokenizer.json  ({tokenizer['vocab_size']} tokens)")