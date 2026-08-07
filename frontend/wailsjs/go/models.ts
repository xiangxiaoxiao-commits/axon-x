export namespace main {
	
	export class MemoryEntry {
	    id: number;
	    conversationId: string;
	    summary: string;
	    embedModel: string;
	    dim: number;
	    createdAt: number;
	    updatedAt: number;
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.conversationId = source["conversationId"];
	        this.summary = source["summary"];
	        this.embedModel = source["embedModel"];
	        this.dim = source["dim"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.title = source["title"];
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
	export class Recommendation {
	    taskType: string;
	    tier: string;
	    provider: string;
	    model: string;
	    temperature: number;
	    maxTokens: number;
	    iq: number;
	    costUsd: number;
	    minutes: number;
	    providerName: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Recommendation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.taskType = source["taskType"];
	        this.tier = source["tier"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.temperature = source["temperature"];
	        this.maxTokens = source["maxTokens"];
	        this.iq = source["iq"];
	        this.costUsd = source["costUsd"];
	        this.minutes = source["minutes"];
	        this.providerName = source["providerName"];
	        this.available = source["available"];
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
	export class Memory {
	    id: number;
	    conversationId: string;
	    summary: string;
	    embedModel: string;
	    dim: number;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Memory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.conversationId = source["conversationId"];
	        this.summary = source["summary"];
	        this.embedModel = source["embedModel"];
	        this.dim = source["dim"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class MemoryHit {
	    id: number;
	    conversationId: string;
	    summary: string;
	    embedModel: string;
	    dim: number;
	    createdAt: number;
	    updatedAt: number;
	    score: number;
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryHit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.conversationId = source["conversationId"];
	        this.summary = source["summary"];
	        this.embedModel = source["embedModel"];
	        this.dim = source["dim"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.score = source["score"];
	        this.title = source["title"];
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

export namespace routing {
	
	export class Recommendation {
	    provider: string;
	    model: string;
	    temperature: number;
	    maxTokens: number;
	    iq: number;
	    costUsd: number;
	    minutes: number;
	
	    static createFrom(source: any = {}) {
	        return new Recommendation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.temperature = source["temperature"];
	        this.maxTokens = source["maxTokens"];
	        this.iq = source["iq"];
	        this.costUsd = source["costUsd"];
	        this.minutes = source["minutes"];
	    }
	}
	export class TaskProfile {
	    key: string;
	    title: string;
	    primary: Recommendation;
	    alternate: Recommendation;
	
	    static createFrom(source: any = {}) {
	        return new TaskProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.title = source["title"];
	        this.primary = this.convertValues(source["primary"], Recommendation);
	        this.alternate = this.convertValues(source["alternate"], Recommendation);
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
	export class Table {
	    profiles: Record<string, TaskProfile>;
	    order: string[];
	
	    static createFrom(source: any = {}) {
	        return new Table(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profiles = this.convertValues(source["profiles"], TaskProfile, true);
	        this.order = source["order"];
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

