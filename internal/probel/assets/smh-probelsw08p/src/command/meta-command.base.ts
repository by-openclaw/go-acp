import { JsonUtility } from '../common/utility/json.utility';
import { CommandIdentifier } from './command-contract';
import { CommandPropertyHolderBase } from './command-property-holder';
import { MetaInnerCommand } from './meta-inner-base';

export abstract class MetaCommandBase<TParams, TOptions> extends CommandPropertyHolderBase<TParams, TOptions> {
    private _commands: Array<MetaInnerCommand<TParams, TOptions>>;

    protected constructor(identifier: CommandIdentifier, params: TParams, options: TOptions = <any>{}) {
        super(identifier, params, options);
        this._commands = new Array<MetaInnerCommand<TParams, TOptions>>();
    }

    get commands(): Array<MetaInnerCommand<TParams, TOptions>> {
        return this._commands;
    }

    buildCommand(): void {
        const dataBuffers: Buffer[] = this.buildData();
        for (let index = 0; index < dataBuffers.length; index++) {
            const dataBuffer = dataBuffers[index];
            const command: MetaInnerCommand<TParams, TOptions> = new MetaInnerCommand(
                this.identifier,
                this.params,
                this.options,
                this.toLogDescription()
            );
            command.buildCommand(dataBuffer);
            this._commands.push(command);
        }
    }

    toJson(): string {
        return JsonUtility.stringify({
            name: this.name,
            description: this.getCommandDescription(),
            commands: this.commands.map(c => c.toDisplay())
        });
    }

    protected abstract buildData(): Array<Buffer>;
    protected abstract toLogDescription(): string;
}
