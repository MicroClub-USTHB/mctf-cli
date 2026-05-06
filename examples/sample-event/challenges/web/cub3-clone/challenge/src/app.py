import os
from flask import Flask, jsonify

app = Flask(__name__)


@app.route("/")
def index():
    return "Solve the cube. /solve when you're done.", 200


@app.route("/solve", methods=["POST"])
def solve():
    # Cube state validation omitted from the example; the real challenge
    # checks for a solved state before returning the flag.
    flag_path = os.environ.get("FLAG_FILE", "/app/flag.txt")
    with open(flag_path) as f:
        return jsonify({"flag": f.read().strip()})
