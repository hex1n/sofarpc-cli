import socket, sys
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", 21299)); s.listen(8)
sys.stderr.write("DEAF_READY 21299\n"); sys.stderr.flush()
conns = []
while True:
    c, _ = s.accept()          # accept and never answer
    conns.append(c)
