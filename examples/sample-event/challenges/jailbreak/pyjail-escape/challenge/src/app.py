import socket

def handle_client(conn):
    try:
        conn.sendall(b"Welcome to PyJail! Enter your python expression:\n> ")
        data = conn.recv(1024)
        if not data:
            return
        cmd = data.decode("utf-8").strip()
        safe_builtins = {"print": print}
        result = eval(cmd, {"__builtins__": safe_builtins})
        conn.sendall(f"Result: {result}\n".encode("utf-8"))
    except Exception as e:
        conn.sendall(f"Error: {e}\n".encode("utf-8"))
    finally:
        conn.close()

def main():
    server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind(("0.0.0.0", 8000))
    server.listen(5)
    while True:
        conn, _ = server.accept()
        handle_client(conn)

if __name__ == "__main__":
    main()
