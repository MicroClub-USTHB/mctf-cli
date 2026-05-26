"""Starter app. Replace with your challenge's actual server."""
import os

from flask import Flask

app = Flask(__name__)


@app.route("/")
def index():
    return "hello, world", 200


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=int(os.environ.get("PORT", "8000")))
