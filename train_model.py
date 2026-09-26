"""
Train the next-call predictor on synthetic_api_calls.csv and save
cache_predict_model.keras. Mirrors the notebook architecture
(Embedding -> LSTM -> softmax) but is repeatable from the CLI.

Token ids are assigned by sklearn LabelEncoder over all call strings;
convert_to_onnx.py rebuilds the SAME mapping into tokenizer.json.
"""

import numpy as np
import pandas as pd
from sklearn.preprocessing import LabelEncoder
from tensorflow.keras.models import Sequential
from tensorflow.keras.layers import LSTM, Dense, Embedding
from tensorflow.keras.preprocessing.sequence import pad_sequences

MAX_LEN = 8  # must match convert_to_onnx.py and tokenizer.json
EPOCHS = 30
BATCH = 64


def main():
    df = pd.read_csv("synthetic_api_calls.csv")

    all_calls = pd.concat([df["current_call"], df["next_call"]]).unique()
    le = LabelEncoder().fit(all_calls)
    vocab_size = len(le.classes_)
    print(f"vocab size: {vocab_size}")

    df["x_id"] = le.transform(df["current_call"])
    df["y_id"] = le.transform(df["next_call"])

    # each session [A, B, C] -> ([A] -> B), ([A, B] -> C)
    sessions = (
        df.sort_values(["session_id", "step"])
        .groupby("session_id")["x_id"]
        .apply(list)
        .tolist()
    )
    X_list, y_list = [], []
    for s in sessions:
        for t in range(1, len(s)):
            X_list.append(s[:t])
            y_list.append(s[t])

    X_pad = pad_sequences(X_list, maxlen=MAX_LEN, padding="pre")
    y_arr = np.array(y_list)
    print(f"training pairs: {len(X_pad)}")

    model = Sequential([
        Embedding(vocab_size, 32, mask_zero=True),
        LSTM(64),
        Dense(vocab_size, activation="softmax"),
    ])
    model.compile(loss="sparse_categorical_crossentropy", optimizer="adam", metrics=["accuracy"])
    model.fit(X_pad, y_arr, epochs=EPOCHS, batch_size=BATCH, validation_split=0.2, verbose=2)

    model.save("cache_predict_model.keras")
    print("saved cache_predict_model.keras")

    # quick sanity: the two transitions the user cares about
    def predict_next(calls, top=3):
        ids = le.transform(calls)
        ids_pad = pad_sequences([ids], maxlen=MAX_LEN, padding="pre")
        probs = model.predict(ids_pad, verbose=0)[0]
        top_idx = probs.argsort()[-top:][::-1]
        return [(le.inverse_transform([i])[0], round(float(probs[i]), 3)) for i in top_idx]

    for hist in (
        ["GET /cart"],
        ["GET /checkout"],
        ["GET /products", "GET /products?id=44", "POST /cart"],
        ["GET /products", "GET /products?id=44", "POST /wishlist"],
    ):
        print(f"\nafter {hist}:")
        for call, p in predict_next(hist):
            print(f"  {p * 100:5.1f}%  {call}")


if __name__ == "__main__":
    main()
