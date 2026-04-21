import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandOptionsUtility, ProtectTallyCommandOptions } from './option';
import { CommandParamsUtility, ProtectTallyCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Protect Tally Message command
 *
 * This message is issued by a controller in response to a PROTECT INTERROGATE message (Command Byte 10).
 * It returns the current protect status of a destination.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class ProtectTallyCommand
 * @extends {CommandBase<ProtectTallyCommandParams>}
 */
export class ProtectTallyCommand extends CommandBase<ProtectTallyCommandParams, ProtectTallyCommandOptions> {
    /**
     *Creates an instance of ProtectTallyCommand.
     * @param {ProtectTallyCommandParams} params the command parameters
     * @param {ProtectTallyCommandOptions} _options the command options
     * @memberof ProtectTallyCommand
     */
    constructor(params: ProtectTallyCommandParams, options: ProtectTallyCommandOptions) {
        super(ProtectTallyCommand.getCommandId(params), params, options);
        this.validateParams(params);
    }
    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId, levelId are 4 bits coded destination < 895 and deviceId > 1023
     * + Extended : true
     *
     * @private
     * @static
     * @param {ProtectTallyCommandParams} params the command parameters
     * @returns {boolean} 'true' if the command is extended otherwise 'false'
     * @memberof ProtectTallyCommand
     */
    private static isExtended(params: ProtectTallyCommandParams): boolean {
        return params.matrixId > 15 || params.levelId > 15 || params.destinationId > 895 || params.deviceId > 1023;
    }

    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {ProtectTallyCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof ProtectTallyCommand
     */
    private static getCommandId(params: ProtectTallyCommandParams): CommandIdentifier {
        return ProtectTallyCommand.isExtended(params)
            ? CommandIdentifiers.TX.EXTENDED.PROTECT_TALLY_MESSAGE
            : CommandIdentifiers.TX.GENERAL.PROTECT_TALLY_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof ProtectTallyCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return ProtectTallyCommand.isExtended(this.params) ? descriptionFor('Extended') : descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectMessageCommand
     */
    protected buildData(): Buffer {
        return ProtectTallyCommand.isExtended(this.params) ? this.buildDataExtended() : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     * @private
     * @param {ProtectTallyCommandParams} the command parameters
     * @memberof ProtectTallyCommand
     */
    private validateParams(params: ProtectTallyCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Builds the normal command
     * Returns Probel SW-P-08 - General Protect Tally Message CMD_011_0X0b
     *
     * + This message is issued by a controller in response to a PROTECT INTERROGATE message (Command Byte 10).
     * + It returns the current protect status of a destination.
     *
     * | Message | Command Byte  | 011 - 0x0b                                                                                                                        |
     * |---------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format  | Notes                                                                                                                             |
     * | Byte 1  | Matrix/Level  | Matrix/Level number as defined in 3.1.2                                                                                           |
     * | Byte 2  | Protect       | Protect details                                                                                                                   |
     * |         | 0             | Not Protected                                                                                                                     |
     * |         | 1             | Pro-Bel Protected                                                                                                                 |
     * |         | 2             | Pro-Bel override Protected (Cannot be altered remotely)                                                                           |
     * |         | 3             | OEM Protected                                                                                                                     |
     * | Byte 3  | Multiplier    | Multiplier as defined in 3.1.6                                                                                                    |
     * | Byte 4  | Dest number   | Destination number MOD 128                                                                                                        |
     * | Byte 5  | Device number | Device number MOD 128                                                                                                             |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof ProtectTallyCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 6 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(this.options.protectDetails)
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(this.params.destinationId, this.params.deviceId))
            .writeUInt8(this.params.destinationId % 128)
            .writeUInt8(this.params.deviceId % 128)
            .toBuffer();
    }

    /**
     * Build the extended command
     * Returns Probel SW-P-08 - Extended Protect Tally Message CMD_139_0X8b
     *
     * + This message is issued by a controller in response to an EXTENDED PROTECT INTERROGATE message (Command Byte 138).
     * + It returns the current protect status of a destination.
     *
     * | Message | Command Byte  | 139 - 0x8b                                                                                                                         |
     * |---------|----------------------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format  | Notes                                                                                                                              |
     * | Byte 1  | Matrix number |                                                                                                                                    |
     * | Byte 2  | Level number  |                                                                                                                                    |
     * | Byte3   | Protect       | Protect details as defined in 3.2.5                                                                                                |
     * | Byte 4  | Dest mult     | Destination number multiplier DIV 256                                                                                              |
     * | Byte 5  | Dest num      | Destination number MOD 256                                                                                                         |
     * | Byte 6  | Device mult   | Device number DIV 256                                                                                                              |
     * | Byte 7  | Device num    | Device number MOD 256                                                                                                              |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof ProtectTallyCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 8 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(this.options.protectDetails)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .writeUInt8(Math.floor(this.params.deviceId / 256))
            .writeUInt8(this.params.deviceId % 256)
            .toBuffer();
    }
}
