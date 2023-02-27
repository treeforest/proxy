rm *.pem

# 1.Generate CA's private key and self-signed certificate
openssl req -x509 -newkey rsa:4096 -days 3650 -nodes -keyout ca-key.pem -out ca-cert.pem -subj "/C=CN/ST=FuZhou"

echo "CA's self-signed certificate"
openssl x509 -in ca-cert.pem -noout -text

# 2.Generate web server's private key and certificate signing request (CSR)
openssl req -newkey rsa:4096 -nodes -keyout server-key.pem -out server-req.pem -subj "/C=CN/ST=FuZhou"

# 3.Use CA's private key to sign web server's CSR and get back the signed certificate
openssl x509 -req -in server-req.pem -days 3650 -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial -out server-cert.pem -extfile server-ext.cnf

echo "Server's self-signed certificate"
openssl x509 -in server-cert.pem -noout -text

echo "Verify ca-cert.pem server-cert.pem"
openssl verify -CAfile ca-cert.pem server-cert.pem

# 4.Generate web server's private key and certificate signing request (CSR)
openssl req -newkey rsa:4096 -nodes -keyout proxy-key.pem -out proxy-req.pem -subj "/C=CN/ST=FuZhou"

# 5.Use CA's private key to sign web server's CSR and get back the signed certificate
openssl x509 -req -in proxy-req.pem -days 3650 -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial -out proxy-cert.pem -extfile proxy-ext.cnf

echo "client's self-signed certificate"
openssl x509 -in proxy-cert.pem -noout -text