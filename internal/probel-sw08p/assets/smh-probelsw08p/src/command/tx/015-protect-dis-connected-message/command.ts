import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandOptionsUtility, ProtectDiscConnectedCommandOptions } from './options';
import { CommandParamsUtility, ProtectDiscConnectedCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Protect Dis-Connected Message command
 *
 * This command is issued by the controller when the protect data is altered i.e. a destination has been unprotected and also if the data was unsuccessfully altered as a result of a PROTECT DIS-CONNECT message (Command Bytes 14).
 * This message is broadcast on all ports.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class ProtectDiscConnectedCommand
 * @extends {CommandBase<ProtectDiscConnectedCommandParams>}
 */
export class ProtectDiscConnectedCommand extends CommandBase<
    ProtectDiscConnectedCommandParams,
    ProtectDiscConnectedCommandOptions
> {
    /**
     * Creates an instance of ProtectDiscConnectedCommand.
     * @param {ProtectDiscConnectedCommandParams} params the command parameters
     * @param {ProtectDiscConnectedCommandOptions} _options the command options
     * @memberof ProtectDiscConnectedCommand
     */
    constructor(params: ProtectDiscConnectedCommandParams, options: ProtectDiscConnectedCommandOptions) {
        super(ProtectDiscConnectedCommand.getCommandId(params), params, options);
        this.validateParams(params);
    }
    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId, levelId are 4 bits coded destination < 895 and deviceId > 1023
     * + Extended : true
     *
     * @private
     * @static
     * @param {ProtectDiscConnectedCommandParams} params the command parameters
     * @returns {boolean} 'true' if the command is extended otherwise 'false'
     * @memberof ProtectDiscConnectedCommand
     */
    private static isExtended(params: ProtectDiscConnectedCommandParams): boolean {
        return params.matrixId > 15 || params.levelId > 15 || params.destinationId > 895 || params.deviceId > 1023;
    }
    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {ProtectDiscConnectedCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof ProtectDiscConnectedCommand
     */
    private static getCommandId(params: ProtectDiscConnectedCommandParams): CommandIdentifier {
        return ProtectDiscConnectedCommand.isExtended(params)
            ? CommandIdentifiers.TX.EXTENDED.PROTECT_DIS_CONNECTED_MESSAGE
            : CommandIdentifiers.TX.GENERAL.PROTECT_DIS_CONNECTED_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof ProtectDiscConnectedCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return ProtectDiscConnectedCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectDiscConnectedCommand
     */
    protected buildData(): Buffer {
        return ProtectDiscConnectedCommand.isExtended(this.params) ? this.buildDataExtended() : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     * @private
     * @param {ProtectDiscConnectedCommandParams} params command parameters
     * @memberof ProtectDiscConnectedCommand
     */
    private validateParams(params: ProtectDiscConnectedCommandParams): void {
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
     * Returns Probel SW-P-08 - Protect Dis-Connected Message CMD_015_0X0f
     *
     * + This command is issued by the controller when the protect data is altered i.e. a destination has been unprotected and also if the data was unsuccessfully altered as a result of a PROTECT DIS-CONNECT message (Command Bytes 14).
     * + This message is broadcast on all ports.
     *
     * | Message | Command Byte  | 015 - 0x0f                                                                                                                        |
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
     * @memberof ProtectDiscConnectedCommand
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
     * Returns Probel SW-P-08 - Extended Protect Dis-Connected Message CMD_143_0X8f
     *
     * + This command is issued by the controller when the protect data is altered i.e. a destination has been unprotected and also if the data was unsuccessfully altered as a result of an EXTENDED PROTECT DIS-CONNECT message (Command Bytes 142).
     * + This message is broadcast on all ports.
     *
     * | Message | Command Byte  | 143 - 0x8f                                                                                                                         |
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
     * @memberof ProtectDiscConnectedCommand
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
