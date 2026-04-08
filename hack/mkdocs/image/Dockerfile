FROM python:3.13-alpine

# Install dependencies
COPY requirements.txt /tmp/
RUN pip install --no-cache-dir -r /tmp/requirements.txt

# Set working directory
WORKDIR /work

# Copy entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Expose port for local development
EXPOSE 3000

# Set entrypoint
ENTRYPOINT ["/entrypoint.sh"]
CMD ["build"]
