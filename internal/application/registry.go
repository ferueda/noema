package application

type StaticAgentRegistry struct {
	agents map[string]Agent
}

func NewStaticAgentRegistry(agents ...Agent) StaticAgentRegistry {
	registry := StaticAgentRegistry{agents: make(map[string]Agent, len(agents))}
	for _, agent := range agents {
		registry.agents[agent.Name()+"@"+agent.Version()] = agent
	}
	return registry
}

func (registry StaticAgentRegistry) Find(name, version string) (Agent, bool) {
	agent, ok := registry.agents[name+"@"+version]
	return agent, ok
}
