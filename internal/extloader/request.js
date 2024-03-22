function request(data) {
	let _r = _make_request({
		Method: data.method,
		URL: data.url,
		Headers: data.headers,
		Body: data.body
	});
	return {
		content_length: _r.ContentLength,
		body: _r.Body,
		status_code: _r.StatusCode,
		headers: new Headers(_r.Headers)
	}
}