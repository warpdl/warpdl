class Headers {
	constructor(data) {
		this.headers = data;
	}

	append(name, value) {
		this.headers.Append(name, value);
	}

	delete(name) {
		this.headers.Delete(name);
	}

	get(name) {
		return this.headers.Get(name);
	}

	getSetCookies() {
		return this.headers.GetSetCookies();
	}

	has(name) {
		return this.headers.Has(name)
	}

	set(name, value) {
		this.headers.Set(name, value);
	}

	forEach(callback) {
		this.headers.ForEach(callback);
	}

	keys() {
		return this.headers.Keys();
	}

	values() {
		return this.headers.Values();
	}

	entries() {
		return this.headers.Entries();
	}

	[Symbol.iterator]() {
		this.headers[Symbol.iterator]();
	}

	get size() {
		return this.headers.Size;
	}

	get [Symbol.toStringTag]() {
		return this.headers.Get(Symbol.toStringTag);
	}
}