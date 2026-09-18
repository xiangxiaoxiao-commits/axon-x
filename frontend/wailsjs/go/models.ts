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
	    namespace: string;
	    sessionCount: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slug = source["slug"];
	        this.path = source["path"];
	        this.namespace = source["namespace"];
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
	    file: string;
	    namespace: string;
	
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
	        this.file = source["file"];
	        this.namespace = source["namespace"];
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
	
	export class Assertion {
	    text: string;
	    state?: string;
	    confidence?: number;
	    sources?: string[];
	    observedAt?: number;
	    validUntil?: number;
	
	    static createFrom(source: any = {}) {
	        return new Assertion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.state = source["state"];
	        this.confidence = source["confidence"];
	        this.sources = source["sources"];
	        this.observedAt = source["observedAt"];
	        this.validUntil = source["validUntil"];
	    }
	}
	export class BundleStats {
	    entities: number;
	    relations: number;
	    observations: number;
	    consideredEntities?: number;
	    omittedChunks?: number;
	    omittedEmbeddings?: number;
	    omittedProvenance?: number;
	    omittedReviews?: number;
	    droppedRelations?: number;
	    droppedAssertions?: number;
	    droppedEntities?: number;
	    mergedAfterScrub?: number;
	
	    static createFrom(source: any = {}) {
	        return new BundleStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entities = source["entities"];
	        this.relations = source["relations"];
	        this.observations = source["observations"];
	        this.consideredEntities = source["consideredEntities"];
	        this.omittedChunks = source["omittedChunks"];
	        this.omittedEmbeddings = source["omittedEmbeddings"];
	        this.omittedProvenance = source["omittedProvenance"];
	        this.omittedReviews = source["omittedReviews"];
	        this.droppedRelations = source["droppedRelations"];
	        this.droppedAssertions = source["droppedAssertions"];
	        this.droppedEntities = source["droppedEntities"];
	        this.mergedAfterScrub = source["mergedAfterScrub"];
	    }
	}
	export class Entity {
	    name: string;
	    type: string;
	    observations: string[];
	    aliases?: string[];
	    obsSources?: string[];
	    assertions?: Assertion[];
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
	        this.assertions = this.convertValues(source["assertions"], Assertion);
	        this.embedding = source["embedding"];
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
	export class Relation {
	    from: string;
	    to: string;
	    label: string;
	    inferred?: boolean;
	    confidence?: number;
	    state?: string;
	    sources?: string[];
	    evidence?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Relation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = source["from"];
	        this.to = source["to"];
	        this.label = source["label"];
	        this.inferred = source["inferred"];
	        this.confidence = source["confidence"];
	        this.state = source["state"];
	        this.sources = source["sources"];
	        this.evidence = source["evidence"];
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
	export class HealthIssue {
	    code: string;
	    level: string;
	    count: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new HealthIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.level = source["level"];
	        this.count = source["count"];
	        this.message = source["message"];
	    }
	}
	export class GraphHealth {
	    score: number;
	    entities: number;
	    assertions: number;
	    trustedAssertions: number;
	    candidateAssertions: number;
	    expiredAssertions: number;
	    conflictingAssertions: number;
	    sourcelessAssertions: number;
	    relations: number;
	    trustedRelations: number;
	    candidateRelations: number;
	    sourcelessRelations: number;
	    isolatedEntities: number;
	    governanceActions: number;
	    canUndo: boolean;
	    issues: HealthIssue[];
	
	    static createFrom(source: any = {}) {
	        return new GraphHealth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.score = source["score"];
	        this.entities = source["entities"];
	        this.assertions = source["assertions"];
	        this.trustedAssertions = source["trustedAssertions"];
	        this.candidateAssertions = source["candidateAssertions"];
	        this.expiredAssertions = source["expiredAssertions"];
	        this.conflictingAssertions = source["conflictingAssertions"];
	        this.sourcelessAssertions = source["sourcelessAssertions"];
	        this.relations = source["relations"];
	        this.trustedRelations = source["trustedRelations"];
	        this.candidateRelations = source["candidateRelations"];
	        this.sourcelessRelations = source["sourcelessRelations"];
	        this.isolatedEntities = source["isolatedEntities"];
	        this.governanceActions = source["governanceActions"];
	        this.canUndo = source["canUndo"];
	        this.issues = this.convertValues(source["issues"], HealthIssue);
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
	
	export class BundleInfo {
	    path: string;
	    title: string;
	    version: number;
	    exportedAt: number;
	    scrubLevel: string;
	    stats: graph.BundleStats;
	    redactions: sanitize.Hit[];
	    notes: string[];
	    suggestedNamespace: string;
	
	    static createFrom(source: any = {}) {
	        return new BundleInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.title = source["title"];
	        this.version = source["version"];
	        this.exportedAt = source["exportedAt"];
	        this.scrubLevel = source["scrubLevel"];
	        this.stats = this.convertValues(source["stats"], graph.BundleStats);
	        this.redactions = this.convertValues(source["redactions"], sanitize.Hit);
	        this.notes = source["notes"];
	        this.suggestedNamespace = source["suggestedNamespace"];
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
	export class DistilledObservation {
	    text: string;
	    excluded: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DistilledObservation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.excluded = source["excluded"];
	    }
	}
	export class DistilledEntity {
	    name: string;
	    type: string;
	    observations: DistilledObservation[];
	
	    static createFrom(source: any = {}) {
	        return new DistilledEntity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.observations = this.convertValues(source["observations"], DistilledObservation);
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
	export class FactPair {
	    before: string;
	    after: string;
	
	    static createFrom(source: any = {}) {
	        return new FactPair(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.before = source["before"];
	        this.after = source["after"];
	    }
	}
	export class PreviewEntity {
	    name: string;
	    type: string;
	    aliases?: string[];
	    facts: FactPair[];
	    redacted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PreviewEntity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.aliases = source["aliases"];
	        this.facts = this.convertValues(source["facts"], FactPair);
	        this.redacted = source["redacted"];
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
	export class ExportPreview {
	    namespace: string;
	    title: string;
	    level: string;
	    stats: graph.BundleStats;
	    redactions: sanitize.Hit[];
	    entities: PreviewEntity[];
	    notes: string[];
	    suggestedName: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.namespace = source["namespace"];
	        this.title = source["title"];
	        this.level = source["level"];
	        this.stats = this.convertValues(source["stats"], graph.BundleStats);
	        this.redactions = this.convertValues(source["redactions"], sanitize.Hit);
	        this.entities = this.convertValues(source["entities"], PreviewEntity);
	        this.notes = source["notes"];
	        this.suggestedName = source["suggestedName"];
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
	export class ExportResult {
	    canceled: boolean;
	    path: string;
	    namespace: string;
	    level: string;
	    stats: graph.BundleStats;
	    redactions: sanitize.Hit[];
	    sizeBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canceled = source["canceled"];
	        this.path = source["path"];
	        this.namespace = source["namespace"];
	        this.level = source["level"];
	        this.stats = this.convertValues(source["stats"], graph.BundleStats);
	        this.redactions = this.convertValues(source["redactions"], sanitize.Hit);
	        this.sizeBytes = source["sizeBytes"];
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
	
	export class ImportResult {
	    namespace: string;
	    bundleId: string;
	    title: string;
	    added: graph.BundleStats;
	    redactions: sanitize.Hit[];
	    replaced: boolean;
	    mergedInto: boolean;
	    embeddedCount: number;
	    embedError?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.namespace = source["namespace"];
	        this.bundleId = source["bundleId"];
	        this.title = source["title"];
	        this.added = this.convertValues(source["added"], graph.BundleStats);
	        this.redactions = this.convertValues(source["redactions"], sanitize.Hit);
	        this.replaced = source["replaced"];
	        this.mergedInto = source["mergedInto"];
	        this.embeddedCount = source["embeddedCount"];
	        this.embedError = source["embedError"];
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
	export class ImportedBundle {
	    id: string;
	    sessionId: string;
	    entities: number;
	    relations: number;
	    observations: number;
	
	    static createFrom(source: any = {}) {
	        return new ImportedBundle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sessionId = source["sessionId"];
	        this.entities = source["entities"];
	        this.relations = source["relations"];
	        this.observations = source["observations"];
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
	export class Namespace {
	    name: string;
	    entities: number;
	
	    static createFrom(source: any = {}) {
	        return new Namespace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.entities = source["entities"];
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
	export class ScopeOption {
	    key: string;
	    label: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new ScopeOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.count = source["count"];
	    }
	}
	export class SessionKnowledge {
	    sessionId: string;
	    entities: DistilledEntity[];
	    indexed: boolean;
	    sessionExcluded: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SessionKnowledge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.entities = this.convertValues(source["entities"], DistilledEntity);
	        this.indexed = source["indexed"];
	        this.sessionExcluded = source["sessionExcluded"];
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
	export class SessionList {
	    sessions: claudedata.SessionMeta[];
	    scopes: ScopeOption[];
	    total: number;
	    unassigned: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessions = this.convertValues(source["sessions"], claudedata.SessionMeta);
	        this.scopes = this.convertValues(source["scopes"], ScopeOption);
	        this.total = source["total"];
	        this.unassigned = source["unassigned"];
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

export namespace mcpinstall {
	
	export class Agent {
	    name: string;
	    label: string;
	    format: string;
	
	    static createFrom(source: any = {}) {
	        return new Agent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.format = source["format"];
	    }
	}
	export class AgentStatus {
	    agent: Agent;
	    installed: boolean;
	    command: string;
	    configPath: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent = this.convertValues(source["agent"], Agent);
	        this.installed = source["installed"];
	        this.command = source["command"];
	        this.configPath = source["configPath"];
	        this.error = source["error"];
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

export namespace sanitize {
	
	export class Sample {
	    before: string;
	    after: string;
	
	    static createFrom(source: any = {}) {
	        return new Sample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.before = source["before"];
	        this.after = source["after"];
	    }
	}
	export class Hit {
	    rule: string;
	    description: string;
	    count: number;
	    samples?: Sample[];
	
	    static createFrom(source: any = {}) {
	        return new Hit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rule = source["rule"];
	        this.description = source["description"];
	        this.count = source["count"];
	        this.samples = this.convertValues(source["samples"], Sample);
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

