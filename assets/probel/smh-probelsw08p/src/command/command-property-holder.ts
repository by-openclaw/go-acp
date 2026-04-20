import Container from 'typedi';

import { LocaleData } from '../common/locale-data/locale-data.model';
import { CommandIdentifier, RxTxType } from './command-contract';
import { LocaleDataCache } from './command-locale-data-cache';

export abstract class CommandPropertyHolderBase<TParams, TOptions> {
    protected constructor(
        private readonly _identifier: CommandIdentifier,
        private readonly _params: TParams,
        private readonly _options: TOptions = <any>{}
    ) {}

    static get commandLocalDataCache(): LocaleDataCache {
        return Container.get(LocaleDataCache);
    }

    get identifier(): CommandIdentifier {
        return this._identifier;
    }

    get id(): number {
        return this._identifier.id;
    }

    get params(): TParams {
        return this._params;
    }

    get options(): TOptions {
        return this._options;
    }
    get name(): string {
        return this._identifier.name;
    }

    get rxTxType(): RxTxType {
        return this._identifier.rxTxType;
    }

    get isExtended(): boolean {
        return this._identifier.isExtended;
    }

    async getCommandDescription(): Promise<string> {
        let localeData: LocaleData;

        if (this.rxTxType === 'RX') {
            localeData = this.isExtended
                ? CommandPropertyHolderBase.commandLocalDataCache.getRxExtendedCommandLocaleData(this.name)
                : CommandPropertyHolderBase.commandLocalDataCache.getRxGeneralCommandLocalData(this.name);
        } else {
            localeData = this.isExtended
                ? CommandPropertyHolderBase.commandLocalDataCache.getTxExtendedCommandData(this.name)
                : CommandPropertyHolderBase.commandLocalDataCache.getTxGeneralCommandLocaleData(this.name);
        }
        return localeData.description;
    }
}
