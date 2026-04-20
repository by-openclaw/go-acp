import { NotImplementedError } from '../common/error/not-implemented.error';
import { CommandIdentifier } from './command-contract';
import { CommandBase } from './command.base';

export class MetaInnerCommand<TParams, TOptions> extends CommandBase<TParams, TOptions> {
    constructor(identifier: CommandIdentifier, params: TParams, options: TOptions, private _logDescription: string) {
        super(identifier, params, options);
    }

    buildData(): Buffer {
        throw new NotImplementedError();
    }

    toLogDescription(): string {
        return this._logDescription;
    }
}
