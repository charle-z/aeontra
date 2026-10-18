package mcpserver

// WithHTTPSessionStore installs the service-owned durable HTTP session store.
// Production configures one store below the shared state root; isolated tests may
// omit it and receive a private ephemeral store.
func (s *Server) WithHTTPSessionStore(store *HTTPSessionStore) *Server {
	if s != nil {
		s.httpSessions = store
	}
	return s
}

func (s *Server) configuredHTTPSessionStore() *HTTPSessionStore {
	if s != nil && s.httpSessions != nil {
		return s.httpSessions
	}
	return newHTTPSessionStore(defaultHTTPSessionTTL)
}
