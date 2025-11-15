FROM eclipse-temurin:8-jdk

WORKDIR /app

RUN apt-get update \
 && apt-get install -y curl libsaxon-java \
 && rm -rf /var/lib/apt/lists/*

RUN curl -fSL -o spectest.xml "https://gnosis.cx/download/gnosis/xml/relax/spectest.xml" \
 && curl -fSL -o split.xsl   "https://gnosis.cx/download/gnosis/xml/relax/split.xsl"

RUN mkdir /out

CMD java -cp /usr/share/java/saxon.jar com.icl.saxon.StyleSheet spectest.xml split.xsl dir=/out
