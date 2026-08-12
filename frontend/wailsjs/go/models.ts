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
	export class KnowledgeMatch {
	    names: string[];
	    context: string;
	    sources?: string[];
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.names = source["names"];
	        this.context = source["context"];
	        this.sources = source["sources"];
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

