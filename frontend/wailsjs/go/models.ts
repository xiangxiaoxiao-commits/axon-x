export namespace claudedata {
	
	export class MemoryFile {
	    scope: string;
	    name: string;
	    path: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.content = source["content"];
	    }
	}
	export class Project {
	    slug: string;
	    path: string;
	    sessionCount: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slug = source["slug"];
	        this.path = source["path"];
	        this.sessionCount = source["sessionCount"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class SessionMessage {
	    role: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.text = source["text"];
	    }
	}
	export class SessionMeta {
	    id: string;
	    projectSlug: string;
	    title: string;
	    messageCount: number;
	    updatedAt: number;
	    sizeBytes: number;
	    cwd: string;
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectSlug = source["projectSlug"];
	        this.title = source["title"];
	        this.messageCount = source["messageCount"];
	        this.updatedAt = source["updatedAt"];
	        this.sizeBytes = source["sizeBytes"];
	        this.cwd = source["cwd"];
	        this.model = source["model"];
	    }
	}
	export class SessionProgress {
	    lastUser: string;
	    lastAssistant: string;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lastUser = source["lastUser"];
	        this.lastAssistant = source["lastAssistant"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace gitx {
	
	export class FileChange {
	    path: string;
	    status: string;
	    staged: boolean;
	    unstaged: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.status = source["status"];
	        this.staged = source["staged"];
	        this.unstaged = source["unstaged"];
	    }
	}
	export class RepoStatus {
	    isRepo: boolean;
	    root: string;
	    branch: string;
	    changes: FileChange[];
	    hasStaged: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RepoStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isRepo = source["isRepo"];
	        this.root = source["root"];
	        this.branch = source["branch"];
	        this.changes = this.convertValues(source["changes"], FileChange);
	        this.hasStaged = source["hasStaged"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace graph {
	
	export class Entity {
	    name: string;
	    type: string;
	    observations: string[];
	    aliases?: string[];
	    obsSources?: string[];
	    embedding?: number[];
	
	    static createFrom(source: any = {}) {
	        return new Entity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.observations = source["observations"];
	        this.aliases = source["aliases"];
	        this.obsSources = source["obsSources"];
	        this.embedding = source["embedding"];
	    }
	}
	export class Relation {
	    from: string;
	    to: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new Relation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = source["from"];
	        this.to = source["to"];
	        this.label = source["label"];
	    }
	}
	export class Graph {
	    projectSlug: string;
	    entities: Entity[];
	    relations: Relation[];
	    updatedAt: number;
	    sourceSessions: string[];
	
	    static createFrom(source: any = {}) {
	        return new Graph(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectSlug = source["projectSlug"];
	        this.entities = this.convertValues(source["entities"], Entity);
	        this.relations = this.convertValues(source["relations"], Relation);
	        this.updatedAt = source["updatedAt"];
	        this.sourceSessions = source["sourceSessions"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class CommitDraft {
	    message: string;
	    prTitle: string;
	    prBody: string;
	    usedKnowledge: string[];
	    knowledgeSources: string[];
	    truncated: boolean;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new CommitDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.prTitle = source["prTitle"];
	        this.prBody = source["prBody"];
	        this.usedKnowledge = source["usedKnowledge"];
	        this.knowledgeSources = source["knowledgeSources"];
	        this.truncated = source["truncated"];
	        this.warnings = source["warnings"];
	    }
	}
	export class EmbeddingConfig {
	    provider: string;
	    model: string;
	    mode: string;
	
	    static createFrom(source: any = {}) {
	        return new EmbeddingConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.mode = source["mode"];
	    }
	}
	export class InjectedChunk {
	    text: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new InjectedChunk(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.source = source["source"];
	    }
	}
	export class KnowledgeMatch {
	    names: string[];
	    context: string;
	    chunks?: InjectedChunk[];
	    chunkHits?: number;
	    sources?: string[];
	    method?: string;
	    semanticSeeds?: string[];
	    keywordHits?: string[];
	    local?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.names = source["names"];
	        this.context = source["context"];
	        this.chunks = this.convertValues(source["chunks"], InjectedChunk);
	        this.chunkHits = source["chunkHits"];
	        this.sources = source["sources"];
	        this.method = source["method"];
	        this.semanticSeeds = source["semanticSeeds"];
	        this.keywordHits = source["keywordHits"];
	        this.local = source["local"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderInfo {
	    name: string;
	    protocol: string;
	    baseUrl: string;
	    keyRef: string;
	    hasKey: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.protocol = source["protocol"];
	        this.baseUrl = source["baseUrl"];
	        this.keyRef = source["keyRef"];
	        this.hasKey = source["hasKey"];
	    }
	}

}

export namespace mcpinstall {
	
	export class Status {
	    installed: boolean;
	    command: string;
	    configPath: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.command = source["command"];
	        this.configPath = source["configPath"];
	    }
	}

}

export namespace model {
	
	export class Conversation {
	    id: string;
	    title: string;
	    taskType: string;
	    model: string;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Conversation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.taskType = source["taskType"];
	        this.model = source["model"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Message {
	    id: number;
	    conversationId: string;
	    role: string;
	    content: string;
	    model: string;
	    promptTokens: number;
	    completionTokens: number;
	    status: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.conversationId = source["conversationId"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.model = source["model"];
	        this.promptTokens = source["promptTokens"];
	        this.completionTokens = source["completionTokens"];
	        this.status = source["status"];
	        this.createdAt = source["createdAt"];
	    }
	}

}

export namespace provider {
	
	export class Config {
	    name: string;
	    protocol: string;
	    baseUrl: string;
	    keyRef: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.protocol = source["protocol"];
	        this.baseUrl = source["baseUrl"];
	        this.keyRef = source["keyRef"];
	    }
	}

}

export namespace search {
	
	export class Hit {
	    projectSlug: string;
	    sessionId: string;
	    title: string;
	    role: string;
	    snippet: string;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Hit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectSlug = source["projectSlug"];
	        this.sessionId = source["sessionId"];
	        this.title = source["title"];
	        this.role = source["role"];
	        this.snippet = source["snippet"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace task {
	
	export class Run {
	    id: number;
	    taskId: string;
	    seq: number;
	    provider: string;
	    model: string;
	    feedback: string;
	    result: string;
	    status: string;
	    error?: string;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Run(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.taskId = source["taskId"];
	        this.seq = source["seq"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.feedback = source["feedback"];
	        this.result = source["result"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Spec {
	    goal: string;
	    background: string;
	    constraints: string[];
	    scope: string[];
	    acceptCriteria: string[];
	    missedPoints: string[];
	    steps: string[];
	    injectedKnowledge: string[];
	    knowledgeSources: string[];
	    recallMethod?: string;
	    recallLocal?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Spec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.goal = source["goal"];
	        this.background = source["background"];
	        this.constraints = source["constraints"];
	        this.scope = source["scope"];
	        this.acceptCriteria = source["acceptCriteria"];
	        this.missedPoints = source["missedPoints"];
	        this.steps = source["steps"];
	        this.injectedKnowledge = source["injectedKnowledge"];
	        this.knowledgeSources = source["knowledgeSources"];
	        this.recallMethod = source["recallMethod"];
	        this.recallLocal = source["recallLocal"];
	    }
	}
	export class Task {
	    id: string;
	    title: string;
	    input: string;
	    spec: Spec;
	    status: string;
	    failedStage?: string;
	    provider: string;
	    model: string;
	    projectSlug: string;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.input = source["input"];
	        this.spec = this.convertValues(source["spec"], Spec);
	        this.status = source["status"];
	        this.failedStage = source["failedStage"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.projectSlug = source["projectSlug"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

